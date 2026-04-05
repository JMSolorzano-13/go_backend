# SAT Request Management Tools (Go Backend)

## Overview

Go CLI tools equivalent to the Python scripts for managing SAT WebService requests:
- `sat-request-generator`: Creates new SAT query requests (CFDI/METADATA)
- Future: `sat-batch-reprocessor`: Reprocesses existing SENT/DOWNLOADED queries

## 1. SAT Request Generator

**Binary:** `go_backend/bin/sat-request-generator`  
**Source:** `go_backend/cmd/sat_request_generator/main.go`

### Purpose
Generates SAT WebService requests by sending messages to LocalStack SQS `queue_create_query`. Creates chunks of CFDI and METADATA requests for specified date ranges.

### Features
- Lists all companies from database
- Validates S3 certificates (warning only, doesn't block)
- Creates CFDI chunks (every 60 days) and METADATA chunks (every 180 days)
- Both ISSUED and RECEIVED types for each chunk
- Real-time verification polling
- Waits for all queries to reach terminal states

### Usage

```bash
cd go_backend
./bin/sat-request-generator
```

**Interactive prompts:**
1. Company ID or UUID
2. Start date (YYYY-MM-DD)
3. End date (YYYY-MM-DD)
4. Confirmation (yes/no)

### Test Script

```bash
cd go_backend
./test_sat_generator.sh
```

Pre-configured test for company `6450eba4-4715-4181-9a1a-7949f9c8cf1f` with Q1 2026 dates.

### Example Output

```
=== SAT WebService Request Generator ===

Available companies:
  [2] SIE200729UA0  (6450eba4-4715-4181-9a1a-7949f9c8cf1f)

Company ID or UUID: 6450eba4-4715-4181-9a1a-7949f9c8cf1f

Selected: [2] SIE200729UA0 (6450eba4-471...)
  ⚠️  WARNING: Certificates not found in S3 (ws_1/c_2.*)
  Bucket: solucioncp-certs-local
  Continuing anyway (workers will fail if certs are missing)...
  Start date (YYYY-MM-DD): 2026-01-01
  End date   (YYYY-MM-DD): 2026-03-31

--- Plan ---
  CFDI     : 2 chunks x 2 (ISSUED+RECEIVED) = 4 requests  (every 60d)
  METADATA : 1 chunks x 2 (ISSUED+RECEIVED) = 2 requests  (every 180d)
  Total    : 6 SQS messages -> queue_create_query

  CFDI       1/2  2026-01-01 -> 2026-03-02
  CFDI       2/2  2026-03-02 -> 2026-03-31
  METADATA   1/1  2026-01-01 -> 2026-03-31

Send 6 messages? (yes/no): yes

6 messages sent. Waiting for worker to process...

  [0s] 2/6 queries in terminal state...
  [5s] 3/6 queries in terminal state...
  [10s] 5/6 queries in terminal state...
  [15s] 6/6 queries in terminal state...
============================================================
  RESULTS: 6 OK  /  0 ERRORS  /  6 expected
============================================================
  [OK] 1fc5daa4-dc7...     CFDI RECEIVED  -> SENT
  [OK] e435eeaf-03e...     CFDI   ISSUED  -> SENT
  [OK] c7d9b426-efe...     CFDI RECEIVED  -> SENT
  [OK] 82a52efd-e80...     CFDI   ISSUED  -> SENT
  [OK] e3670916-cd7... METADATA RECEIVED  -> SENT
  [OK] 3af0a7da-b47... METADATA   ISSUED  -> SENT

  All 6 queries created successfully.
```

## 2. Verification of Results

### Database Query

```sql
SELECT 
    LEFT(identifier::text, 12) as id,
    state, 
    request_type, 
    download_type,
    TO_CHAR(start, 'YYYY-MM-DD') as start_date,
    TO_CHAR("end", 'YYYY-MM-DD') as end_date,
    TO_CHAR(created_at, 'YYYY-MM-DD HH24:MI:SS') as created,
    is_manual
FROM "6450eba4-4715-4181-9a1a-7949f9c8cf1f".sat_query
ORDER BY created_at DESC
LIMIT 10;
```

### Terminal States

**OK States:**
- `SENT` - Query sent to SAT
- `DOWNLOADED` - Packages downloaded
- `PROCESSED` - CFDIs extracted and processed
- `TO_DOWNLOAD` - Ready to download
- `SPLITTED` - Query split into smaller chunks

**Error States:**
- `ERROR_IN_CERTS` - Certificate problem
- `ERROR_SAT_WS_INTERNAL` - SAT internal error
- `ERROR_SAT_WS_UNKNOWN` - Unknown SAT error
- `TIME_LIMIT_REACHED` - Timeout
- `INFORMATION_NOT_FOUND` - No data for date range

## Tested Configuration

- **Company:** `6450eba4-4715-4181-9a1a-7949f9c8cf1f` (SIE200729UA0)
- **Test Date Range:** 2026-01-01 to 2026-03-31 (Q1 2026)
- **Existing Queries:** 2 (pre-existing)
- **New Queries Created:** 6
- **Total Queries:** 8
- **Success Rate:** 100% (all queries reached SENT state)

## Building from Source

```bash
cd go_backend
go build -o bin/sat-request-generator ./cmd/sat_request_generator
```

## Dependencies

- PostgreSQL database (ezaudita_db)
- LocalStack SQS (queue_create_query)
- LocalStack S3 (solucioncp-certs-local)
- SQS Worker (local_sqs_worker.py) running

## Configuration

All configuration is hardcoded for local development:
- DB: `postgresql://solcpuser:local_dev_password@localhost:5432/ezaudita_db?sslmode=disable`
- SQS Endpoint: `http://localhost:4566`
- Queue URL: `http://localhost:4566/000000000000/queue_create_query`
- S3 Bucket: `solucioncp-certs-local`

## Future Enhancements

1. **SAT Batch Reprocessor** (equivalent to `manual_batch_reprocess.py`)
   - Reprocess SENT/DOWNLOADED queries
   - Batch processing with timeout
   - State monitoring and reporting

2. **Environment Configuration**
   - Read from `.env.local` or environment variables
   - Support for different environments (dev/staging/prod)

3. **Advanced Filtering**
   - Filter queries by date range
   - Filter by state
   - Filter by request type

4. **Export Functionality**
   - Export query results to JSON/CSV
   - Generate reports
