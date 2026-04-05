# go_backend — Siigo Fiscal Go API

Full-parity replacement of the Python/Chalice backend. Implements all 114 production endpoints using Go 1.22+ `net/http`, `uptrace/bun` ORM, and AWS SDK v2.

## Tech Stack

| Layer | Choice |
|---|---|
| Language | Go 1.22+ |
| HTTP router | `net/http.ServeMux` (pattern-based, Go 1.22) |
| ORM | `uptrace/bun` + `lib/pq` |
| Auth | JWT (Cognito JWKS) via `golang-jwt/jwt/v5` |
| AWS | SDK v2 — SQS, S3, Cognito-IDP |
| Billing | `stripe-go/v76` |
| Excel exports | `github.com/xuri/excelize/v2` |
| Config | `.env` (via `joho/godotenv`) |

## Local Development

### Prerequisites

- Docker Desktop running
- Go 1.22+
- Shared `.env` (symlinked from `fastapi_backend/.env`)

### Start Everything

```bash
# From repo root
./start-local.sh go
```

This starts PostgreSQL + LocalStack via Docker Compose, then builds and runs the Go backend on `:8001`.

### Manual Start

```bash
cd go_backend
go build -o bin/server ./cmd/server
LOCAL_INFRA=true ./bin/server
```

### Environment

The backend reads its config from `go_backend/.env` (symlinked to `fastapi_backend/.env`). Required keys:

```
DB_HOST / DB_PORT / DB_USER / DB_PASS / DB_NAME
AWS_REGION / AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
AWS_ENDPOINT_URL          # LocalStack: http://localhost:4566
S3_ADD / S3_EXPORT_BUCKET
SQS_*                     # ~25 queue URLs
COGNITO_USER_POOL_ID / COGNITO_CLIENT_ID
ADMIN_EMAILS              # comma-separated list
STRIPE_SECRET_KEY
LOCAL_INFRA=true          # enables dev/login + CORS middleware
```

## Endpoint Groups

| Group | Count | Auth |
|---|---|---|
| Status / Health | 3 | None |
| Dev (local only) | 4 | None (gated by `LOCAL_INFRA`) |
| Company | 11 | `RequireAuth` / `RequireCompany` / `RequireAdminCreate` |
| User | 15 | Mixed |
| CFDI | 22 | `RequireCompany` |
| SAT Query | 5 | `RequireCompany` / `RequireAuth` |
| Poliza | 3 | `RequireCompany` |
| DoctoRelacionado | 3 | `RequireCompany` |
| Attachment | 4 | `RequireAuth` / `RequireCompany` |
| EFOS | 3 | `RequireAuth` / `RequireCompany` |
| Export/CFDIExcluded | 2 | `RequireCompany` |
| Scraper | 2 | `RequireCompany` / `RequireAuth` |
| License | 6 | `RequireAuth` |
| Pasto/ADD webhooks | 13 | None / `RequireAuth` / `RequireCompany` |
| COI | 4 | `RequireCompany` |
| Param / RegimenFiscal | 2 | None |
| Product | 2 | None / `RequireAuth` |
| Permission | 2 | `RequireAuth` |
| Notification | 2 | `RequireAuth` |
| Workspace | 6 | `RequireAuth` / `RequireAdminCreate` |

**Total: 114 endpoints**

## Auth Middleware

| Middleware | What it checks |
|---|---|
| `RequireAuth` | Valid JWT in `access_token` header |
| `RequireCompany` | Valid JWT + `company_identifier` header with user permission |
| `RequireAdmin` | Valid JWT + email in `ADMIN_EMAILS` |
| `RequireAdminCreate` | Valid JWT + admin email OR company permission |

## Performance Baseline (local Docker, 5-run average)

| Endpoint | Avg Latency |
|---|---|
| `GET /api/status/health/api` | <1 ms |
| `GET /api/dev/users` | 2 ms |
| `POST /api/Company/search` | 2 ms |
| `POST /api/CFDI/search` | 4 ms |
| `POST /api/CFDI/get_iva_all` | 16 ms |
| `POST /api/SATQuery/search` | 4 ms |
| `POST /api/DoctoRelacionado/search` | 4 ms |
| `POST /api/Poliza/search` | 4 ms |
| `GET /api/Product/get_all` | <1 ms |
| `POST /api/Pasto/Company/search` | 3 ms |

## Parity Validation

```bash
# From repo root — requires Go backend running on :8001
bash validate_phase12.sh go
```

Expected: **117 PASS, 0 FAIL** (1 SKIP: SQS rate check).

## Project Structure

```
go_backend/
├── cmd/server/         # main.go — wires config, DB, bus, HTTP server
├── internal/
│   ├── config/         # Config struct, .env loading
│   ├── db/             # Database wrapper, tenant schema routing
│   ├── domain/
│   │   ├── auth/       # Admin check, context helpers
│   │   ├── crud/       # Generic Search engine (filters, fuzzy, pagination)
│   │   ├── event/      # EventBus, SQS event types, subscribers
│   │   └── filter/     # Odoo-style domain filter parser
│   ├── handler/        # One file per resource (cfdi.go, company.go, …)
│   ├── infra/
│   │   ├── cognito/    # Cognito-IDP client
│   │   ├── s3/         # S3 client + presign
│   │   └── stripe/     # Stripe client
│   ├── model/
│   │   ├── control/    # Shared-schema models (user, workspace, company, …)
│   │   └── tenant/     # Per-company schema models (cfdi, payment, …)
│   ├── response/       # Standard JSON response helpers
│   └── server/
│       ├── middleware/ # Auth, CORS, logging, recovery
│       └── server.go   # Route registration
```
