package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/adapters/httpclient"
	"github.com/purangoswami2001/hookrelay/internal/adapters/postgres"
	redisadapter "github.com/purangoswami2001/hookrelay/internal/adapters/redis"
	"github.com/purangoswami2001/hookrelay/internal/application/deliveries"
	"github.com/purangoswami2001/hookrelay/internal/config"
	"github.com/purangoswami2001/hookrelay/internal/observability"
	"github.com/purangoswami2001/hookrelay/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Printf("worker stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, closeLogger, err := observability.NewLogger(
		cfg.LogLevel,
		cfg.LogFile,
	)
	if err != nil {
		return err
	}
	defer func() {
		if err := closeLogger(); err != nil {
			log.Printf("close logger: %v", err)
		}
	}()

	logger.Debug("starting workers", "count", cfg.WorkerCount, "redis", cfg.RedisAddr)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	db, err := postgres.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	queue := redisadapter.NewQueue(cfg.RedisAddr)
	defer queue.Close()

	if err := queue.Ping(ctx); err != nil {
		return err
	}

	deliveryRepository := postgres.NewDeliveryRepository(db)
	eventRepository := postgres.NewEventRepository(db)
	subscriptionRepository := postgres.NewSubscriptionRepository(db)

	// 10s per-delivery HTTP timeout
	sender := httpclient.NewSender(10 * time.Second)

	deliveryService := deliveries.NewService(
		deliveryRepository,
		eventRepository,
		subscriptionRepository,
		sender,
		queue,
		logger,
	)

	deliveryWorker := worker.NewDeliveryWorker(
		queue,
		deliveryService,
		logger,
	)

	// Expose Prometheus metrics for workers on port 9091
	metricsServer := &http.Server{
		Addr:    ":9091",
		Handler: observability.MetricsHandler(),
	}

	go func() {
		logger.Info("worker metrics server listening", "addr", ":9091")
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("worker metrics server failed", "error", err)
		}
	}()

	logger.Info(
		"HookRelay worker service starting",
		"worker_count", cfg.WorkerCount,
	)

	var wg sync.WaitGroup

	for i := 1; i <= cfg.WorkerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			logger.Debug("worker spawned", "worker_id", workerID)
			deliveryWorker.Run(ctx, workerID)
		}(i)
	}

	<-ctx.Done()
	logger.Info("draining in-flight deliveries...")

	metricsCtx, cancelMetrics := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelMetrics()
	_ = metricsServer.Shutdown(metricsCtx)

	wg.Wait()
	logger.Info("HookRelay worker service stopped")

	return nil
}

