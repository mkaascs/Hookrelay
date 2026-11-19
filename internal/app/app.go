package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"webhook-delivery/internal/app/postgres"
	"webhook-delivery/internal/app/server"
	"webhook-delivery/internal/config"
	"webhook-delivery/internal/delivery/rest"
	pgRepo "webhook-delivery/internal/infrastructure/postgres"
	"webhook-delivery/internal/infrastructure/prometheus"
	"webhook-delivery/internal/infrastructure/telemetry"
	sloglib "webhook-delivery/internal/lib/logging/slog"
	"webhook-delivery/internal/services/deliveries"
	"webhook-delivery/internal/services/endpoints"
	"webhook-delivery/internal/services/events"
	"webhook-delivery/internal/services/subscriptions"
	"webhook-delivery/internal/services/workers"
)

type App struct {
	Server   *server.Server
	Postgres *postgres.App

	workers    *workers.Worker
	dispatcher *workers.Dispatcher
	workerDone chan struct{}
	telemetry  *telemetry.Telemetry

	log *slog.Logger
	cfg config.Config

	ctx    context.Context
	cancel context.CancelFunc
}

func New(log *slog.Logger, cfg config.Config) *App {
	tracing, err := telemetry.New(context.Background(), cfg.TracingEndpoint)
	if err != nil {
		log.Error("failed to configure tracing", sloglib.Error(err))
		os.Exit(1)
	}

	postgresApp, err := postgres.New(log, cfg.DB, tracing.PostgresTracer())
	if err != nil {
		os.Exit(1)
	}

	postgresApp.MustConnect()
	postgres.MustMigrate(log, cfg.DB)

	metrics := prometheus.New()

	eventRepo := pgRepo.NewEvents(postgresApp.Pool, log, cfg.MaxAttempts)
	endpointRepo := pgRepo.NewEndpoints(postgresApp.Pool, log)
	deliveryRepo := pgRepo.NewDeliveries(postgresApp.Pool, log)
	subscriptionRepo := pgRepo.NewSubscriptions(postgresApp.Pool, log)

	worker := workers.NewWorkers(deliveryRepo, metrics, tracing, cfg.Workers, log)
	dispatcher := workers.NewDispatcher(worker, deliveryRepo, cfg.Dispatcher, log)

	metrics.RegisterQueueDepth(worker.Depth)

	handler := rest.NewHandler(rest.Services{
		Deliveries:    deliveries.NewService(deliveryRepo, dispatcher, log),
		Endpoints:     endpoints.NewService(endpointRepo, log),
		Subscriptions: subscriptions.NewService(subscriptionRepo, log),
		Events:        events.NewService(eventRepo, dispatcher, log),
	})

	ctx, cancel := context.WithCancel(context.Background())

	return &App{
		Server:     server.New(handler, cfg, log, tracing.HTTPMiddleware),
		Postgres:   postgresApp,
		workers:    worker,
		dispatcher: dispatcher,
		workerDone: make(chan struct{}),
		telemetry:  tracing,
		log:        log,
		cfg:        cfg,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (a *App) MustRun() {
	const fn = "app.App.MustRun"
	log := a.log.With(slog.String("fn", fn))

	if err := a.dispatcher.Recover(a.ctx); err != nil {
		log.Error("failed to recover deliveries", sloglib.Error(err))
		os.Exit(1)
	}

	go func() {
		defer close(a.workerDone)
		a.workers.Run(a.ctx)
	}()
	go a.dispatcher.Run(a.ctx)

	log.Info("workers and dispatcher is running", slog.Int("workers_count", a.cfg.MaxGoroutines))

	a.log.Info("http server is running")
	if err := a.Server.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.log.Error("http server failed", sloglib.Error(err))
		os.Exit(1)
	}
}

func (a *App) Shutdown(ctx context.Context) {
	a.cancel()

	if err := a.Server.Shutdown(ctx); err != nil {
		a.log.Error("failed to shutdown http server", sloglib.Error(err))
	}

	select {
	case <-a.workerDone:
	case <-ctx.Done():
		a.log.Warn("failed to wait for workers to stop", sloglib.Error(ctx.Err()))
	}

	if err := a.telemetry.Shutdown(ctx); err != nil {
		a.log.Error("failed to shutdown tracing", sloglib.Error(err))
	}

	a.Postgres.Close()
}
