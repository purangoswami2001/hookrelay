package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/adapters/postgres"
	redisadapter "github.com/purangoswami2001/hookrelay/internal/adapters/redis"
	"github.com/purangoswami2001/hookrelay/internal/config"
	"github.com/purangoswami2001/hookrelay/internal/dispatcher"
	"github.com/purangoswami2001/hookrelay/internal/observability"
)

func main() {
	if err := run(); err != nil {
		log.Printf("dispatcher stopped: %v", err)
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

	logger.Debug("starting dispatcher", "redis", cfg.RedisAddr)

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

	outboxRepository := postgres.NewOutboxRepository(db)

	outboxDispatcher := dispatcher.NewOutboxDispatcher(
		outboxRepository,
		queue,
		logger,
	)

	// Expose Prometheus metrics for dispatcher on port 9092
	metricsServer := &http.Server{
		Addr:    ":9092",
		Handler: observability.MetricsHandler(),
	}

	go func() {
		logger.Info("dispatcher metrics server listening", "addr", ":9092")
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("dispatcher metrics server failed", "error", err)
		}
	}()

	logger.Info("HookRelay dispatcher starting")
	outboxDispatcher.Run(ctx)

	metricsCtx, cancelMetrics := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelMetrics()
	_ = metricsServer.Shutdown(metricsCtx)

	logger.Info("HookRelay dispatcher stopped")

	return nil
}

