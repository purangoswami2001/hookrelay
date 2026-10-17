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
	"github.com/purangoswami2001/hookrelay/internal/application/events"
	"github.com/purangoswami2001/hookrelay/internal/application/subscriptions"
	"github.com/purangoswami2001/hookrelay/internal/config"
	"github.com/purangoswami2001/hookrelay/internal/observability"
	"github.com/purangoswami2001/hookrelay/internal/transport/httpapi"
)

func main() {
	if err := run(); err != nil {
		log.Printf("API stopped: %v", err)
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

	logger.Debug("config loaded", "env", cfg.AppEnv, "addr", cfg.HTTPAddr)

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

	logger.Debug("db connected", "conns", db.Stat().TotalConns())

	eventRepository := postgres.NewEventRepository(db)
	eventService := events.NewService(eventRepository, logger)
	deliveryRepository := postgres.NewDeliveryRepository(db)

	subscriptionRepository := postgres.NewSubscriptionRepository(db)
	subscriptionService := subscriptions.NewService(
		subscriptionRepository,
		logger,
	)

	router := httpapi.NewRouter(
		eventService,
		eventRepository,
		subscriptionService,
		deliveryRepository,
		logger,
	)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)

	go func() {
		logger.Info("HookRelay API starting", "address", cfg.HTTPAddr)

		// pprof: go func() { log.Println(http.ListenAndServe("localhost:6060", nil)) }()

		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		return err

	case <-ctx.Done():
		logger.Info("API shutdown requested")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}

		logger.Info("API shutdown complete")
		return nil
	}
}
