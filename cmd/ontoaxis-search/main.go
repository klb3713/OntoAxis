// Command ontoaxis-search runs the Meilisearch object index worker. Modes:
//
//	ontoaxis-search consume
//	ontoaxis-search rebuild --tenant <uuid> [--object-type <uuid>] [--verify]
//	ontoaxis-search replay [--limit 100]
//	ontoaxis-search migrate
//
// Configuration comes from ONTOAXIS_SEARCH_* and ONTOAXIS_DATABASE_URL.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/platform/search"
	"github.com/klb3713/ontoaxis/internal/platform/search/worker"
)

const envDatabaseURL = "ONTOAXIS_DATABASE_URL"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ontoaxis-search: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	logger := newLogger()
	slog.SetDefault(logger)

	modeName := string(worker.ModeConsume)
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		modeName = args[0]
		args = args[1:]
	}
	mode, err := worker.ParseMode(modeName)
	if err != nil {
		return err
	}

	searchCfg, err := search.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	flags := flag.NewFlagSet("ontoaxis-search", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	tenant := flags.String("tenant", "", "tenant UUID (rebuild)")
	objectType := flags.String("object-type", "", "optional object type UUID filter (rebuild)")
	verify := flags.Bool("verify", false, "verify document counts and content digest after rebuild")
	limit := flags.Int("limit", 100, "maximum dead-letter events to replay")
	flags.Usage = func() {
		_, _ = fmt.Fprintf(flags.Output(), "usage: ontoaxis-search <consume|rebuild|replay|migrate> [flags]\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	workerCfg := worker.Config{
		Search:      searchCfg,
		DatabaseURL: strings.TrimSpace(os.Getenv(envDatabaseURL)),
		Mode:        mode,
		Verify:      *verify,
		ReplayLimit: *limit,
	}
	if strings.TrimSpace(*tenant) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(*tenant))
		if err != nil {
			return fmt.Errorf("invalid --tenant: %w", err)
		}
		workerCfg.Tenant = parsed
	}
	if strings.TrimSpace(*objectType) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(*objectType))
		if err != nil {
			return fmt.Errorf("invalid --object-type: %w", err)
		}
		workerCfg.ObjectTypeID = &parsed
	}
	if mode == worker.ModeRebuild && workerCfg.Tenant == uuid.Nil {
		return fmt.Errorf("--tenant is required for rebuild")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return worker.Run(ctx, workerCfg, logger)
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ONTOAXIS_LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
