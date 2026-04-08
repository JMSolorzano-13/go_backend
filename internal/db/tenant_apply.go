package db

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/uptrace/bun"
)

//go:embed tenant_schema/tenant_tables.sql
var embeddedTenantSQL embed.FS

const tenantSchemaPlaceholder = "__TENANT_SCHEMA_QUOTED__"

// ValidateCompanyTenantSchema ensures identifier is safe for quoted PostgreSQL identifiers (UUID v4).
func ValidateCompanyTenantSchema(schema string) error {
	if len(schema) != 36 {
		return fmt.Errorf("tenant schema: expected UUID length 36, got %d", len(schema))
	}
	for i, c := range schema {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return fmt.Errorf("tenant schema: expected '-' at position %d", i)
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return fmt.Errorf("tenant schema: invalid character %q at %d", c, i)
			}
		}
	}
	return nil
}

// ApplyEmbeddedTenantDDL creates enums, tables, constraints, and indexes for one tenant schema.
// Caller must run CREATE SCHEMA first. SQL is generated from Alembic tenant head (see scripts/regenerate_tenant_tables_sql.sh).
func ApplyEmbeddedTenantDDL(ctx context.Context, bunDB *bun.DB, schema string) error {
	if err := ValidateCompanyTenantSchema(schema); err != nil {
		return err
	}
	raw, err := embeddedTenantSQL.ReadFile("tenant_schema/tenant_tables.sql")
	if err != nil {
		return fmt.Errorf("read embedded tenant sql: %w", err)
	}
	quoted := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"`
	sqlText := strings.ReplaceAll(string(raw), tenantSchemaPlaceholder, quoted)
	if _, err = bunDB.ExecContext(ctx, sqlText); err != nil {
		return err
	}
	// Restore default search_path in case the DDL (from pg_dump) modified session settings.
	_, _ = bunDB.ExecContext(ctx, `SELECT pg_catalog.set_config('search_path', '"$user", public', false)`)
	return nil
}
