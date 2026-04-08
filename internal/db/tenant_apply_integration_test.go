//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func TestApplyEmbeddedTenantDDL_LocalPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = "postgres://solcpuser:local_dev_password@127.0.0.1:5432/ezaudita_db?sslmode=disable"
	}
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	bunDB := bun.NewDB(sqldb, pgdialect.New())
	defer bunDB.Close()

	schema := "aaaaaaaa-bbbb-4ccc-dddd-eeeeeeeeeeee"
	ctx := context.Background()
	_, _ = bunDB.ExecContext(ctx, `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
	_, err := bunDB.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = bunDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
	})

	if err := db.ApplyEmbeddedTenantDDL(ctx, bunDB, schema); err != nil {
		t.Fatalf("ApplyEmbeddedTenantDDL: %v", err)
	}
	var n int
	err = bunDB.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = $1`, schema).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Fatalf("expected several tables, got %d", n)
	}

	// Verify search_path is restored — unqualified "user" must resolve to public.user.
	var sp string
	if err := bunDB.DB.QueryRowContext(ctx, `SHOW search_path`).Scan(&sp); err != nil {
		t.Fatalf("show search_path: %v", err)
	}
	t.Logf("search_path after DDL: %q", sp)

	var userCount int
	err = bunDB.DB.QueryRowContext(ctx, `SELECT count(*) FROM "user"`).Scan(&userCount)
	if err != nil {
		t.Fatalf("query public.user after tenant DDL failed (search_path poisoned): %v", err)
	}
}
