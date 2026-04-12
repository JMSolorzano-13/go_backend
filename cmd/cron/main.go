// Command cron is a one-shot CLI for scheduled SAT sync jobs: enqueue metadata
// sync and/or complete-CFDI work for every active company (control DB filter).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/infra/azsbpub"
	sqsinfra "github.com/siigofiscal/go_backend/internal/infra/sqs"
	"github.com/siigofiscal/go_backend/internal/logger"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

func main() {
	job := flag.String("job", "", "one of: sync-metadata | complete-cfdis | all")
	flag.Parse()

	if err := run(strings.TrimSpace(*job)); err != nil {
		fmt.Fprintf(os.Stderr, "cron: %v\n", err)
		os.Exit(1)
	}
}

func run(job string) error {
	switch job {
	case "sync-metadata", "complete-cfdis", "all":
	default:
		return fmt.Errorf(`invalid -job %q (want sync-metadata, complete-cfdis, or all)`, job)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger.Init(cfg.LogLevel)

	if strings.ToLower(strings.TrimSpace(cfg.CloudProvider)) != "azure" {
		return fmt.Errorf("go-cron: CLOUD_PROVIDER must be azure (got %q)", cfg.CloudProvider)
	}

	database, err := db.New(cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer database.Close()

	pub, cleanup, err := newMessagePublisher(cfg)
	if err != nil {
		return fmt.Errorf("message publisher: %w", err)
	}
	defer cleanup()

	// Synchronous bus so every publish finishes before the process exits (CLI).
	bus := event.NewBus(false)
	sqsinfra.SubscribeAllHandlers(bus, cfg, pub)
	slog.Warn("go-cron: event_bus initialized (sync dispatch)")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	companies, err := listActiveCompanies(ctx, database)
	if err != nil {
		return fmt.Errorf("list companies: %w", err)
	}
	slog.Warn("go-cron: active companies", "count", len(companies), "job", job)

	switch job {
	case "sync-metadata":
		publishMetadata(bus, companies)
	case "complete-cfdis":
		publishCompleteCFDIs(bus, companies)
	case "all":
		publishMetadata(bus, companies)
		publishCompleteCFDIs(bus, companies)
	}

	slog.Warn("go-cron: done", "job", job, "companies", len(companies))
	return nil
}

type activeCompany struct {
	ID          int64  `bun:"id"`
	Identifier  string `bun:"identifier"`
	RFC         string `bun:"rfc"`
	WorkspaceID int64  `bun:"workspace_id"`
}

func listActiveCompanies(ctx context.Context, database *db.Database) ([]activeCompany, error) {
	var rows []activeCompany
	err := database.Primary.NewSelect().
		ColumnExpr("c.id, c.identifier, c.rfc, c.workspace_id").
		TableExpr("company AS c").
		Join("JOIN workspace AS w ON w.id = c.workspace_id").
		Where("c.active = true AND c.have_certificates = true").
		Where("w.valid_until IS NOT NULL AND w.valid_until > NOW()").
		Where("c.rfc IS NOT NULL AND TRIM(c.rfc) <> ''").
		Where("c.workspace_id IS NOT NULL").
		Order("c.id").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func publishMetadata(bus *event.Bus, companies []activeCompany) {
	for _, c := range companies {
		bus.Publish(event.EventTypeSATMetadataRequested, event.SQSCompanySendMetadata{
			CompanyBase:       event.NewCompanyBase(c.Identifier, c.RFC),
			ManuallyTriggered: false,
			WID:               c.WorkspaceID,
			CID:               c.ID,
		})
		slog.Info("go-cron: published SAT_METADATA_REQUESTED", "company", c.Identifier, "cid", c.ID)
	}
}

func publishCompleteCFDIs(bus *event.Bus, companies []activeCompany) {
	for _, c := range companies {
		bus.Publish(event.EventTypeSATCompleteCFDIsNeeded, event.NeedToCompleteCFDIsEvent{
			CompanyBase:  event.NewCompanyBase(c.Identifier, c.RFC),
			DownloadType: tenant.DownloadTypeIssued,
			IsManual:     false,
			Start:        nil,
			End:          nil,
		})
		bus.Publish(event.EventTypeSATCompleteCFDIsNeeded, event.NeedToCompleteCFDIsEvent{
			CompanyBase:  event.NewCompanyBase(c.Identifier, c.RFC),
			DownloadType: tenant.DownloadTypeReceived,
			IsManual:     false,
			Start:        nil,
			End:          nil,
		})
		slog.Info("go-cron: published SAT_COMPLETE_CFDIS_NEEDED ISSUED+RECEIVED", "company", c.Identifier, "cid", c.ID)
	}
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
		slog.Warn("go-cron: publisher=sqs (hybrid local)")
		return sqsinfra.Publisher{Client: sqsc}, noop, nil
	}

	if strings.TrimSpace(cfg.AzureServiceBusConnectionString) != "" {
		sb, err := azsbpub.NewPublisher(cfg.AzureServiceBusConnectionString)
		if err != nil {
			return nil, noop, fmt.Errorf("service bus publisher: %w", err)
		}
		slog.Warn("go-cron: publisher=azure service bus")
		return sb, func() { _ = sb.Close(context.Background()) }, nil
	}

	return nil, noop, fmt.Errorf("go-cron: set AZURE_SERVICEBUS_CONNECTION_STRING or hybrid LocalStack SQS (LOCAL_INFRA + AWS_ENDPOINT_URL)")
}
