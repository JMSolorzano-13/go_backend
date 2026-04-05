package db

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// TenantConn acquires a connection from the pool and sets search_path to the
// given company schema. The caller MUST close the returned *bun.Conn when done.
func (d *Database) TenantConn(ctx context.Context, schema string, readOnly bool) (bun.Conn, error) {
	pool := d.Pool(readOnly)

	conn, err := pool.Conn(ctx)
	if err != nil {
		return bun.Conn{}, fmt.Errorf("acquire conn: %w", err)
	}

	if _, err = conn.ExecContext(ctx, fmt.Sprintf(`SET search_path TO "%s", public`, schema)); err != nil {
		conn.Close()
		return bun.Conn{}, fmt.Errorf("set search_path to %q: %w", schema, err)
	}

	return conn, nil
}

// TenantConnWithTimeout acquires a tenant connection and sets a statement_timeout.
func (d *Database) TenantConnWithTimeout(ctx context.Context, schema string, readOnly bool, timeoutMs int) (bun.Conn, error) {
	conn, err := d.TenantConn(ctx, schema, readOnly)
	if err != nil {
		return bun.Conn{}, err
	}

	if _, err = conn.ExecContext(ctx, fmt.Sprintf("SET statement_timeout TO %d", timeoutMs)); err != nil {
		conn.Close()
		return bun.Conn{}, fmt.Errorf("set statement_timeout: %w", err)
	}

	return conn, nil
}
