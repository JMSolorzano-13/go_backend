-- Fuzzy search (CFDI, EFOS, etc.): filter.ApplyFuzzySearch + cfdi resume use unaccent() + ILIKE.
-- Parity with fastapi_backend/chalicelib/alembic_tenant/versions/e9c67871178d_unaccent.py (tenant)
-- and legacy Aurora ops (terraform/README.md). Extension is database-wide (not per-schema).
CREATE EXTENSION IF NOT EXISTS unaccent;
