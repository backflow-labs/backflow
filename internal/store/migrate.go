package store

import (
	"context"
	"fmt"

	backflowmigrations "github.com/backflow-labs/backflow/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func RunMigrations(ctx context.Context, databaseURL string) error {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse postgres config: %w", err)
	}

	db := stdlib.OpenDB(*config)
	defer db.Close()

	goose.SetBaseFS(backflowmigrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("run goose up: %w", err)
	}
	return nil
}
