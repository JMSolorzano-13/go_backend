package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/extra/bundebug"

	"github.com/siigofiscal/go_backend/internal/config"
)

type Database struct {
	Primary *bun.DB
	Replica *bun.DB
}

func New(cfg *config.Config) (*Database, error) {
	primary, err := openBun(cfg.DBDSN(), cfg, false)
	if err != nil {
		return nil, fmt.Errorf("primary db: %w", err)
	}

	var replica *bun.DB
	if cfg.DBHostRO != "" && cfg.DBHostRO != cfg.DBHost {
		replica, err = openBun(cfg.DBDSNReadOnly(), cfg, true)
		if err != nil {
			primary.Close()
			return nil, fmt.Errorf("replica db: %w", err)
		}
	} else {
		replica = primary
	}

	return &Database{Primary: primary, Replica: replica}, nil
}

func (d *Database) Close() error {
	var firstErr error
	if err := d.Primary.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if d.Replica != d.Primary {
		if err := d.Replica.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (d *Database) Ping(ctx context.Context) error {
	if err := d.Primary.PingContext(ctx); err != nil {
		return fmt.Errorf("primary: %w", err)
	}
	if d.Replica != d.Primary {
		if err := d.Replica.PingContext(ctx); err != nil {
			return fmt.Errorf("replica: %w", err)
		}
	}
	return nil
}

func (d *Database) Pool(readOnly bool) *bun.DB {
	if readOnly {
		return d.Replica
	}
	return d.Primary
}

func openBun(dsn string, cfg *config.Config, readOnly bool) (*bun.DB, error) {
	connector := pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithInsecure(true),
		pgdriver.WithTimeout(30*time.Second),
		pgdriver.WithDialTimeout(5*time.Second),
		pgdriver.WithReadTimeout(30*time.Second),
		pgdriver.WithWriteTimeout(30*time.Second),
	)

	sqldb := sql.OpenDB(connector)
	sqldb.SetMaxOpenConns(10)
	sqldb.SetMaxIdleConns(5)
	sqldb.SetConnMaxLifetime(5 * time.Minute)
	sqldb.SetConnMaxIdleTime(5 * time.Minute)

	bunDB := bun.NewDB(sqldb, pgdialect.New())

	if cfg.DBLogLevel == "DEBUG" {
		bunDB.AddQueryHook(bundebug.NewQueryHook(bundebug.WithVerbose(true)))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := bunDB.PingContext(ctx); err != nil {
		bunDB.Close()
		return nil, err
	}

	role := "primary"
	if readOnly {
		role = "replica"
	}
	slog.Info("database connected", "role", role)

	return bunDB, nil
}
