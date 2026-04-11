// Command test_cfdi_pipeline enqueues one CFDI ISSUED create-query message (1-year window),
// polls sat_query until PROCESSED or a terminal error, then asserts tenant cfdi rows
// populated via process_xml (from_xml=true) in that date range.
//
// Run from go_backend with the same .env as the API/worker (DB_*, SQS_* or Service Bus, etc.):
//
//	go run ./scripts/test_cfdi_pipeline.go
//
// Which database: entirely determined by DB_* in .env (Azure and on-prem are different DB
// instances; company.identifier is the tenant schema UUID, e.g. on-prem 6450eba4-...).
//
// Optional: -company=<uuid> loads that row by identifier only (RFC + workspace_id required);
// default pick still requires active, have_certificates, and workspace valid_until.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	azsbpub "github.com/siigofiscal/go_backend/internal/infra/azsbpub"
	azqueues "github.com/siigofiscal/go_backend/internal/infra/azservicebus"
	sqsinfra "github.com/siigofiscal/go_backend/internal/infra/sqs"
	"github.com/siigofiscal/go_backend/internal/logger"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

func main() {
	companyFlag := flag.String("company", "", "tenant company UUID (= schema name); bypasses strict subscription/certs filter (still needs rfc + workspace_id)")
	pollSeconds := flag.Int("poll", 20, "poll interval seconds for sat_query / cfdi checks")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	logger.Init(cfg.LogLevel)

	database, err := db.New(cfg)
	if err != nil {
		slog.Error("database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	pub, cleanup, err := newMessagePublisher(cfg)
	if err != nil {
		slog.Error("message publisher", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	ctx := context.Background()

	type companyPick struct {
		ID            int64  `bun:"id"`
		Identifier    string `bun:"identifier"`
		RFC           string `bun:"rfc"`
		WorkspaceID   int64  `bun:"workspace_id"`
	}
	var co companyPick
	if s := strings.TrimSpace(*companyFlag); s != "" {
		err := database.Primary.NewSelect().
			ColumnExpr("c.id, c.identifier, c.rfc, c.workspace_id").
			TableExpr("company AS c").
			Where("c.identifier = ?", s).
			Where("c.workspace_id IS NOT NULL").
			Where("c.rfc IS NOT NULL AND TRIM(c.rfc) <> ''").
			Limit(1).
			Scan(ctx, &co)
		if err != nil {
			slog.Error("pick company by -company (check DB_* points to the DB that owns this tenant)", "identifier", s, "error", err)
			os.Exit(1)
		}
		slog.Warn("test_cfdi_pipeline: company from -company (relaxed filter)", "identifier", co.Identifier)
	} else {
		err := database.Primary.NewSelect().
			ColumnExpr("c.id, c.identifier, c.rfc, c.workspace_id").
			TableExpr("company AS c").
			Join("JOIN workspace AS w ON w.id = c.workspace_id").
			Where("c.active = true AND c.have_certificates = true").
			Where("w.valid_until IS NOT NULL AND w.valid_until > NOW()").
			Where("c.rfc IS NOT NULL AND TRIM(c.rfc) <> ''").
			Where("c.workspace_id IS NOT NULL").
			Order("c.id").
			Limit(1).
			Scan(ctx, &co)
		if err != nil {
			slog.Error("pick company (active, certs, valid workspace); use -company=<tenant-uuid> for on-prem dev", "error", err)
			os.Exit(1)
		}
	}

	end := time.Now().UTC()
	start := end.AddDate(-1, 0, 0)

	payload := event.QueryCreateEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: co.Identifier,
		DownloadType:      tenant.DownloadTypeIssued,
		RequestType:       tenant.RequestTypeCFDI,
		IsManual:          true,
		Start:             &start,
		End:               &end,
		WID:               co.WorkspaceID,
		CID:               co.ID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("marshal payload", "error", err)
		os.Exit(1)
	}

	marker := time.Now().UTC().Add(-30 * time.Second)

	in := &port.SendMessageInput{
		QueueURL: cfg.SQSCreateQuery,
		Body:     string(body),
	}
	if isFIFOQueue(cfg.SQSCreateQuery) {
		id := uuid.NewString()
		in.MessageGroupID = id
		in.MessageDeduplicationID = id
	}

	slog.Warn("test_cfdi_pipeline: enqueue",
		"company", co.Identifier,
		"rfc", co.RFC,
		"start", start.Format(time.RFC3339),
		"end", end.Format(time.RFC3339),
		"queue", cfg.SQSCreateQuery,
	)
	if err := pub.SendMessage(ctx, in); err != nil {
		slog.Error("send create_query message", "error", err)
		os.Exit(1)
	}

	poll := time.Duration(*pollSeconds) * time.Second
	if poll < 5*time.Second {
		poll = 5 * time.Second
	}
	deadline := time.Now().Add(cfg.WSMaxWaitingMinutes + 90*time.Minute)

	var lastState string
	var queryID string
	var hintedStartWorker bool
	for time.Now().Before(deadline) {
		conn, err := database.TenantConn(ctx, co.Identifier, true)
		if err != nil {
			slog.Error("tenant conn", "error", err)
			os.Exit(1)
		}

		var row struct {
			Identifier string `bun:"identifier"`
			State      string `bun:"state"`
		}
		err = conn.NewSelect().
			ColumnExpr("identifier, state").
			Model((*tenant.SATQuery)(nil)).
			Where("created_at > ?", marker).
			Where("request_type = ?", tenant.RequestTypeCFDI).
			Where("download_type = ?", tenant.DownloadTypeIssued).
			Order("created_at DESC").
			Limit(1).
			Scan(ctx, &row)
		conn.Close()

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) && !hintedStartWorker {
				hintedStartWorker = true
				slog.Warn("no sat_query row yet — start the Go worker (cmd/worker) so it consumes the create-query queue; same DB and queue URLs as this script")
			}
			slog.Warn("waiting for sat_query row", "error", err, "elapsed", time.Since(marker).Round(time.Second).String())
			time.Sleep(poll)
			continue
		}

		if row.State != lastState {
			slog.Warn("sat_query state", "query_id", row.Identifier, "state", row.State, "elapsed", time.Since(marker).Round(time.Second).String())
			lastState = row.State
		}
		queryID = row.Identifier

		switch row.State {
		case tenant.QueryStateProcessed:
			if err := verifyCFDICount(ctx, database, co.Identifier, start, end); err != nil {
				slog.Error("cfdi verification failed", "error", err)
				os.Exit(1)
			}
			slog.Warn("test_cfdi_pipeline: OK",
				"query_id", queryID,
				"elapsed_total", time.Since(marker).Round(time.Second).String(),
			)
			return
		case tenant.QueryStateErrorInCerts, tenant.QueryStateErrorSATWSUnknown,
			tenant.QueryStateErrorSATWSInternal, tenant.QueryStateErrorTooBig,
			tenant.QueryStateTimeLimitReached, tenant.QueryStateError,
			tenant.QueryStateManuallyCancelled, tenant.QueryStateSplitted,
			tenant.QueryStateInformationNotFound, tenant.QueryStateSubstituted:
			slog.Error("sat_query terminal failure", "query_id", queryID, "state", row.State)
			os.Exit(1)
		default:
			// SENT, TO_DOWNLOAD, DOWNLOADED, DELAYED, PROCESSING, DRAFT, etc. — keep polling
		}

		time.Sleep(poll)
	}

	slog.Error("timeout waiting for PROCESSED", "last_state", lastState, "query_id", queryID, "max_wait", cfg.WSMaxWaitingMinutes)
	os.Exit(1)
}

func verifyCFDICount(ctx context.Context, database *db.Database, companySchema string, start, end time.Time) error {
	conn, err := database.TenantConn(ctx, companySchema, true)
	if err != nil {
		return fmt.Errorf("tenant conn: %w", err)
	}
	defer conn.Close()

	n, err := conn.NewSelect().
		Model((*tenant.CFDI)(nil)).
		Where(`"Fecha" >= ?`, start).
		Where(`"Fecha" <= ?`, end).
		Where("from_xml = ?", true).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("count cfdi: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("expected at least one cfdi row with from_xml=true in [%s, %s], got 0",
			start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	slog.Warn("cfdi rows (from_xml in range)", "count", n)
	return nil
}

func newMessagePublisher(cfg *config.Config) (port.MessagePublisher, func(), error) {
	noop := func() {}
	hybrid := cfg.LocalInfra && strings.TrimSpace(cfg.AWSEndpointURL) != ""
	if hybrid {
		sqsc, err := sqsinfra.NewClient(
			cfg.RegionName,
			cfg.AWSEndpointURL,
			cfg.AWSAccessKeyID,
			cfg.AWSSecretAccessKey,
		)
		if err != nil {
			return nil, noop, fmt.Errorf("sqs hybrid client: %w", err)
		}
		slog.Warn("test_cfdi_pipeline: publisher=sqs (hybrid local)")
		return sqsinfra.Publisher{Client: sqsc}, noop, nil
	}

	switch strings.ToLower(strings.TrimSpace(cfg.CloudProvider)) {
	case "azure":
		if strings.TrimSpace(cfg.AzureServiceBusConnectionString) != "" {
			sb, err := azsbpub.NewPublisher(cfg.AzureServiceBusConnectionString)
			if err != nil {
				return nil, noop, fmt.Errorf("service bus publisher: %w", err)
			}
			slog.Warn("test_cfdi_pipeline: publisher=azure service bus")
			return sb, func() { _ = sb.Close(context.Background()) }, nil
		}
		qp, err := azqueues.NewQueuePublisher(cfg.AzureStorageConnectionString)
		if err != nil {
			return nil, noop, fmt.Errorf("azure storage queue publisher: %w", err)
		}
		slog.Warn("test_cfdi_pipeline: publisher=azure storage queues")
		return qp, noop, nil
	default:
		sqsc, err := sqsinfra.NewClient(
			cfg.RegionName,
			cfg.AWSEndpointURL,
			cfg.AWSAccessKeyID,
			cfg.AWSSecretAccessKey,
		)
		if err != nil {
			return nil, noop, fmt.Errorf("sqs client: %w", err)
		}
		slog.Warn("test_cfdi_pipeline: publisher=sqs")
		return sqsinfra.Publisher{Client: sqsc}, noop, nil
	}
}

func isFIFOQueue(queueURL string) bool {
	parts := strings.Split(queueURL, ".")
	return len(parts) > 0 && parts[len(parts)-1] == "fifo"
}
