package db

import (
	"context"

	"github.com/uptrace/bun"
)

type ctxKey int

const (
	ctxKeyDatabase ctxKey = iota
	ctxKeyTenantConn
)

func WithDatabase(ctx context.Context, d *Database) context.Context {
	return context.WithValue(ctx, ctxKeyDatabase, d)
}

func FromContext(ctx context.Context) *Database {
	d, _ := ctx.Value(ctxKeyDatabase).(*Database)
	return d
}

func ControlDB(ctx context.Context) *bun.DB {
	d := FromContext(ctx)
	if d == nil {
		return nil
	}
	return d.Primary
}

func WithTenantConn(ctx context.Context, conn bun.Conn) context.Context {
	return context.WithValue(ctx, ctxKeyTenantConn, conn)
}

func TenantConnFromCtx(ctx context.Context) (bun.Conn, bool) {
	conn, ok := ctx.Value(ctxKeyTenantConn).(bun.Conn)
	return conn, ok
}
