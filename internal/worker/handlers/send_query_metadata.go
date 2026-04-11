package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/siigofiscal/go_backend/internal/domain/datetime"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// SendQueryMetadata handles SAT_METADATA_REQUESTED messages from
// SQS_SEND_QUERY_METADATA (queue_create_metadata_query).
//
// Mirrors Python sqs_send_query_metadata_listener: publishes two
// SAT_WS_REQUEST_CREATE_QUERY events (METADATA ISSUED and METADATA RECEIVED)
// with the same date window used by QueryCreator for metadata sync.
type SendQueryMetadata struct {
	Deps
}

func (h *SendQueryMetadata) Handle(ctx context.Context, raw json.RawMessage) error {
	_ = ctx
	var msg event.SQSCompanySendMetadata
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("unmarshal SQSCompanySendMetadata: %w", err)
	}

	logger := slog.With(
		"handler", "SendQueryMetadata",
		"company", msg.CompanyIdentifier,
		"manually_triggered", msg.ManuallyTriggered,
	)

	start := datetime.LastXFiscalYearsStart(5)
	end := time.Now().In(datetime.MexicoCity())

	h.Bus.Publish(event.EventTypeSATWSRequestCreateQuery, event.QueryCreateEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: msg.CompanyIdentifier,
		DownloadType:      tenant.DownloadTypeIssued,
		RequestType:       tenant.RequestTypeMetadata,
		IsManual:          msg.ManuallyTriggered,
		Start:             &start,
		End:               &end,
		WID:               msg.WID,
		CID:               msg.CID,
	})

	h.Bus.Publish(event.EventTypeSATWSRequestCreateQuery, event.QueryCreateEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: msg.CompanyIdentifier,
		DownloadType:      tenant.DownloadTypeReceived,
		RequestType:       tenant.RequestTypeMetadata,
		IsManual:          msg.ManuallyTriggered,
		Start:             &start,
		End:               &end,
		WID:               msg.WID,
		CID:               msg.CID,
	})

	logger.Info("published metadata create-query events", "start", start, "end", end)
	return nil
}
