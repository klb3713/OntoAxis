package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/app"
	"github.com/klb3713/ontoaxis/internal/platform/config"
	"github.com/klb3713/ontoaxis/internal/platform/module"
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
	a, err := app.New(ctx, cfg, logger, func(pool *pgxpool.Pool) []module.Module {
		return app.BuildModules(pool, cfg)
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
