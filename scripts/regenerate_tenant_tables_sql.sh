#!/usr/bin/env bash
# Regenerates internal/db/tenant_schema/tenant_tables.sql from the current Alembic TENANT head.
# Requires: Docker (siigo-fiscal-postgres), fastapi_backend + Poetry, run from monorepo ROOT.
#
# Usage (from repo root):
#   bash go_backend/scripts/regenerate_tenant_tables_sql.sh

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PH_SCHEMA="tenant-placeholder-uuid"
OUT="$ROOT/go_backend/internal/db/tenant_schema/tenant_tables.sql"

docker exec siigo-fiscal-postgres psql -U solcpuser -d ezaudita_db -c "DROP SCHEMA IF EXISTS \"${PH_SCHEMA}\" CASCADE;" >/dev/null
docker exec siigo-fiscal-postgres psql -U solcpuser -d ezaudita_db -c "CREATE SCHEMA \"${PH_SCHEMA}\";" >/dev/null

export DB_HOST=127.0.0.1 DB_PORT=5432 DB_NAME=ezaudita_db DB_USER=solcpuser DB_PASSWORD=local_dev_password
(cd "$ROOT/fastapi_backend" && bash ./run_tenant_migration.sh "${PH_SCHEMA}")

docker exec siigo-fiscal-postgres pg_dump -U solcpuser -d ezaudita_db --schema-only --schema="${PH_SCHEMA}" \
  | grep -v '^\\' \
  | sed "/^CREATE SCHEMA \"${PH_SCHEMA}\";/d" \
  | sed "/^ALTER SCHEMA \"${PH_SCHEMA}\" OWNER TO/d" \
  | sed "s/\"${PH_SCHEMA}\"/__TENANT_SCHEMA_QUOTED__/g" \
  >"$OUT"

echo "Wrote $OUT ($(wc -c <"$OUT") bytes)"
