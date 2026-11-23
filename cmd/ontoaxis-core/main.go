package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/klb3713/ontoaxis/internal/app"
	"github.com/klb3713/ontoaxis/internal/platform/config"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	logger, err := observability.NewLogger(cfg.LogLevel)
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a, err := app.New(ctx, cfg, logger, func(assemblyCtx context.Context, pool *pgxpool.Pool, tracer trace.Tracer, assemblyLogger *slog.Logger, metrics *observability.Metrics) (app.RuntimeAssembly, error) {
		return app.BuildRuntimeAssembly(assemblyCtx, pool, cfg, tracer, assemblyLogger, metrics)
	})
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
	if err := a.Run(ctx); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
