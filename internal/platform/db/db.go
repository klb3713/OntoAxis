package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var platformMigrations embed.FS

// Open creates a pgxpool with bounded connections and a startup ping.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// MigratePlatform applies the embedded platform migrations. Migrations are
// idempotent because goose records applied versions in its own table.
func MigratePlatform(ctx context.Context, dsn string) error {
	return MigrateFS(ctx, dsn, platformMigrations, "migrations")
}

// ExecStatements executes a sequence of DDL/DML statements in order. Module
// migrations use this small helper because Module.Migrate receives the shared
// pgx pool rather than a database URL.
func ExecStatements(ctx context.Context, pool *pgxpool.Pool, statements ...string) error {
	for i, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("execute migration statement %d: %w", i+1, err)
		}
	}
	return nil
}

// MigrateFS runs goose migrations from a filesystem. It is used for platform
// and module migrations; each module owns its own migration directory.
func MigrateFS(ctx context.Context, dsn string, fsys fs.FS, dir string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()
	goose.SetBaseFS(fsys)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, dir); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
