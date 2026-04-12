package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/infra/sat"
	"github.com/siigofiscal/go_backend/internal/model/control"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// CreateQuery handles SAT_WS_REQUEST_CREATE_QUERY messages.
//
// Pipeline step 1: load FIEL → create sat_query row (DRAFT) → send SolicitaDescarga
// to SAT → update row (SENT) → publish SAT_WS_QUERY_SENT for verification.
//
// Mirrors Python QuerySenderWS._send + mark_as_sent / mark_as_error_*.
type CreateQuery struct {
	Deps
}

func (h *CreateQuery) Handle(ctx context.Context, raw json.RawMessage) error {
	var msg CreateQueryMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("unmarshal CreateQueryMsg: %w", err)
	}

	logger := slog.With(
		"handler", "CreateQuery",
		"company", msg.CompanyIdentifier,
		"download_type", msg.DownloadType,
		"request_type", msg.RequestType,
	)

	// 1. Create the sat_query row in DRAFT state.
	queryID := uuid.NewString()
	now := time.Now().UTC()

	conn, err := h.DB.TenantConn(ctx, msg.CompanyIdentifier, false)
	if err != nil {
		return fmt.Errorf("tenant conn: %w", err)
	}
	defer conn.Close()

	isManual := msg.IsManual
	sq := &tenant.SATQuery{
		Identifier:   queryID,
		Name:         "",
		Start:        derefTime(msg.Start, now.AddDate(0, 0, -30)),
		End:          derefTime(msg.End, now),
		DownloadType: msg.DownloadType,
		RequestType:  msg.RequestType,
		State:        tenant.QueryStateDraft,
		IsManual:     &isManual,
		Technology:   tenant.SATTechWebService,
		CreatedAt:    now,
	}

	if _, err := conn.NewInsert().Model(sq).Exec(ctx); err != nil {
		return fmt.Errorf("insert sat_query: %w", err)
	}

	logger.Warn("sat_query created", "query_id", queryID)

	// 2. Load FIEL and create SAT connector.
	connector, err := h.loadFIEL(ctx, msg.WID, msg.CID)
	if err != nil {
		var certsErr *sat.CertsNotFoundError
		if errors.As(err, &certsErr) {
			return h.markErrorInCerts(ctx, conn, queryID, msg.CompanyIdentifier, logger)
		}
		return h.markError(ctx, conn, queryID, tenant.QueryStateErrorSATWSInternal, logger, err)
	}

	// 3. Send SolicitaDescarga to SAT.
	dlType := mapDownloadType(msg.DownloadType)
	reqType := mapRequestType(msg.RequestType)

	satQuery := sat.NewQuery(dlType, reqType, sq.Start, sq.End)
	if err := satQuery.Send(connector); err != nil {
		return h.markError(ctx, conn, queryID, tenant.QueryStateErrorSATWSInternal, logger, err)
	}

	// 4. Update sat_query: state=SENT, name=SAT identifier.
	sentDate := time.Now().UTC()
	if _, err := conn.NewUpdate().
		Model((*tenant.SATQuery)(nil)).
		Set("state = ?", tenant.QueryStateSent).
		Set("name = ?", satQuery.Identifier).
		Set("sent_date = ?", sentDate).
		Set("updated_at = ?", sentDate).
		Where("identifier = ?", queryID).
		Exec(ctx); err != nil {
		return fmt.Errorf("update sat_query to SENT: %w", err)
	}

	logger.Warn("sat_query sent", "sat_id", satQuery.Identifier)

	// 5. Publish SAT_WS_QUERY_SENT → triggers verify.
	h.Bus.Publish(event.EventTypeSATWSQuerySent, event.QueryVerifyEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: msg.CompanyIdentifier,
		QueryIdentifier:   queryID,
		DownloadType:      msg.DownloadType,
		RequestType:       msg.RequestType,
		Start:             sq.Start,
		End:               sq.End,
		State:             tenant.QueryStateSent,
		Name:              satQuery.Identifier,
		SentDate:          sentDate,
		IsManual:          msg.IsManual,
		WID:               msg.WID,
		CID:               msg.CID,
	})

	return nil
}

// markErrorInCerts sets the query to ERROR_IN_CERTS and marks company has_valid_certs=false.
func (h *CreateQuery) markErrorInCerts(ctx context.Context, conn bun.Conn, queryID, companyID string, logger *slog.Logger) error {
	logger.Warn("cert error, marking ERROR_IN_CERTS")

	if _, err := conn.NewUpdate().
		Model((*tenant.SATQuery)(nil)).
		Set("state = ?", tenant.QueryStateErrorInCerts).
		Set("updated_at = ?", time.Now().UTC()).
		Where("identifier = ?", queryID).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark ERROR_IN_CERTS: %w", err)
	}

	// Update company.has_valid_certs = false in control DB.
	if _, err := h.DB.Primary.NewUpdate().
		Model((*control.Company)(nil)).
		Set("has_valid_certs = ?", false).
		Where("identifier = ?", companyID).
		Exec(ctx); err != nil {
		logger.Error("failed to update company has_valid_certs", "error", err)
	}

	return nil
}

// markError sets the query to a given error state.
func (h *CreateQuery) markError(ctx context.Context, conn bun.Conn, queryID, state string, logger *slog.Logger, cause error) error {
	logger.Error("marking query error", "state", state, "cause", cause)

	if _, err := conn.NewUpdate().
		Model((*tenant.SATQuery)(nil)).
		Set("state = ?", state).
		Set("updated_at = ?", time.Now().UTC()).
		Where("identifier = ?", queryID).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark %s: %w", state, err)
	}

	return nil
}

// mapDownloadType converts the domain string to the SAT SOAP enum.
func mapDownloadType(dt string) sat.DownloadType {
	switch dt {
	case tenant.DownloadTypeIssued:
		return sat.DownloadTypeIssued
	case tenant.DownloadTypeReceived:
		return sat.DownloadTypeReceived
	default:
		return sat.DownloadType(dt)
	}
}

// mapRequestType converts the domain string to the SAT SOAP enum.
func mapRequestType(rt string) sat.RequestType {
	switch rt {
	case tenant.RequestTypeMetadata:
		return sat.RequestTypeMetadata
	case tenant.RequestTypeCFDI:
		return sat.RequestTypeCFDI
	default:
		return sat.RequestType(rt)
	}
}

func derefTime(t *time.Time, fallback time.Time) time.Time {
	if t != nil {
		return *t
	}
	return fallback
}
