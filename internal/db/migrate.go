package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type migration struct {
	version string
	name    string
	sql     string
}

// RunMigrations applies pending SQL migrations from the embedded migrations/ directory.
// Files must be named like 001_description.sql and are applied in lexicographic order.
func RunMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version   VARCHAR(255) PRIMARY KEY,
			name      VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		parts := strings.SplitN(e.Name(), "_", 2)
		version := parts[0]
		name := strings.TrimSuffix(e.Name(), ".sql")
		migrations = append(migrations, migration{version: version, name: name, sql: string(data)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })

	applied := make(map[string]bool)
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		rows.Scan(&v)
		applied[v] = true
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		slog.Info("applying migration", "version", m.version, "name", m.name)
		start := time.Now()

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("execute migration %s: %w", m.name, err)
		}
		// Reference catalog rows (embedded CSV); parity with Alembic bef098e1f688 + 84c3a8e301b6.
		if m.version == "002" {
			if err := SeedCatalogs(ctx, tx); err != nil {
				tx.Rollback()
				return fmt.Errorf("seed catalogs for %s: %w", m.name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", m.version, m.name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
		slog.Info("migration applied", "version", m.version, "elapsed", time.Since(start))
	}

	return nil
}
