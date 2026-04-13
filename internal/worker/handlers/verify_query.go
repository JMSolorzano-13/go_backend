package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/infra/sat"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

const (
	// maxPackages is the sanity limit on the number of packages per query.
	maxPackages = 200

	// maxCFDIQtyInQuery is the sanity limit on CFDIs for non-metadata queries.
	maxCFDIQtyInQuery = 500_000

	// defaultWSMaxWait is used if WSMaxWaitingMinutes is misconfigured to zero.
	defaultWSMaxWait = 240 * time.Minute

	// VerifyQueryStatusCode constants matching Python VerifyQueryStatusCode.
	verifyStatusCodeInfoNotFound = 5004
	verifyStatusCodeMaxLimit     = 5002
)

// verifyBackoffDelay returns how long to wait before the next verify attempt,
// based on elapsed time since sent_date: 15m, then 30m, then 60m (repeating).
func verifyBackoffDelay(elapsed time.Duration) time.Duration {
	switch {
	case elapsed < 15*time.Minute:
		return 15 * time.Minute
	case elapsed < 45*time.Minute:
		return 30 * time.Minute
	default:
		return 60 * time.Minute
	}
}

// VerifyQuery handles SAT_WS_QUERY_SENT / SAT_WS_QUERY_VERIFY_NEEDED messages.
//
// Pipeline step 2: load FIEL → call VerificaSolicitud on SAT → route by status:
//   - FINISHED → publish SAT_WS_QUERY_DOWNLOAD_READY
//   - IN_PROCESS/ACCEPTED/UNKNOWN → re-publish SAT_WS_QUERY_VERIFY_NEEDED (incremental backoff until WS max wait)
//   - ERROR/REJECTED/EXPIRED → mark error state
//
// Mirrors Python QueryVerifierWS.parallel_verify + action dispatch.
type VerifyQuery struct {
	Deps
}

func (h *VerifyQuery) Handle(ctx context.Context, raw json.RawMessage) error {
	var msg VerifyQueryMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("unmarshal VerifyQueryMsg: %w", err)
	}

	if err := h.enrichSentDateIfMissing(ctx, &msg); err != nil {
		return err
	}

	logger := slog.With(
		"handler", "VerifyQuery",
		"company", msg.CompanyIdentifier,
		"query", msg.QueryIdentifier,
		"sat_id", msg.Name,
	)

	// 1. Load FIEL.
	connector, err := h.loadFIEL(ctx, msg.WID, msg.CID)
	if err != nil {
		logger.Error("failed to load FIEL for verify, retrying", "error", err)
		return h.retryOrTimeout(msg, logger)
	}

	// 2. Call VerificaSolicitud.
	satQuery, err := connector.VerifyRequest(msg.Name, mapRequestType(msg.RequestType))
	if err != nil {
		var reqErr *sat.RequestError
		if errors.As(err, &reqErr) {
			logger.Error("SAT verify request error", "status", reqErr.StatusCode, "reason", reqErr.Reason)
		} else {
			logger.Error("SAT verify error", "error", err)
		}
		return h.retryOrTimeout(msg, logger)
	}

	elapsedVerifyMS := int64(0)
	if !msg.SentDate.IsZero() {
		elapsedVerifyMS = time.Since(msg.SentDate).Milliseconds()
	}

	logger.Warn("SAT verify result",
		"estado", satQuery.QueryStatus,
		"cfdi_qty", satQuery.CfdiQty,
		"packages", len(satQuery.Packages),
		"status_code", satQuery.StatusCode,
		"mensaje", satQuery.Message,
		"cod_estatus", satQuery.CodEstatus,
		"elapsed_since_sent_ms", elapsedVerifyMS,
		"download_type", msg.DownloadType,
		"request_type", msg.RequestType,
	)

	if satQuery.QueryStatus == sat.VerifyStatusError ||
		satQuery.QueryStatus == sat.VerifyStatusRejected ||
		satQuery.QueryStatus == sat.VerifyStatusExpired {
		// #region agent log
		agentDebugLog("H1", "verify_query.go:Handle", "verify terminal error-class status", map[string]any{
			"estado_solicitud":        int(satQuery.QueryStatus),
			"codigo_estado_solicitud": satQuery.StatusCode,
			"cfdi_qty":                satQuery.CfdiQty,
			"packages_n":              len(satQuery.Packages),
			"mensaje":                 satQuery.Message,
			"cod_estatus":             satQuery.CodEstatus,
			"elapsed_since_sent_ms":   elapsedVerifyMS,
			"request_type":            msg.RequestType,
			"download_type":           msg.DownloadType,
			"sat_id":                  msg.Name,
		})
		// #endregion
	}

	// 3. Route by EstadoSolicitud.
	switch {
	case satQuery.QueryStatus == sat.VerifyStatusFinished:
		return h.handleFinished(ctx, msg, satQuery, logger)

	case satQuery.QueryStatus == sat.VerifyStatusAccepted ||
		satQuery.QueryStatus == sat.VerifyStatusInProcess ||
		satQuery.QueryStatus == sat.VerifyStatusUnknown:
		return h.retryOrTimeout(msg, logger)

	case satQuery.QueryStatus == sat.VerifyStatusError ||
		satQuery.QueryStatus == sat.VerifyStatusRejected ||
		satQuery.QueryStatus == sat.VerifyStatusExpired:
		return h.handleError(ctx, msg, satQuery, logger)

	default:
		logger.Warn("unexpected verify status", "status", satQuery.QueryStatus)
		return h.retryOrTimeout(msg, logger)
	}
}

// handleFinished processes a FINISHED verification: check sanity limits, then
// publish SAT_WS_QUERY_DOWNLOAD_READY.
func (h *VerifyQuery) handleFinished(ctx context.Context, msg VerifyQueryMsg, sq *sat.Query, logger *slog.Logger) error {
	packageIDs := make([]string, len(sq.Packages))
	for i, pkg := range sq.Packages {
		packageIDs[i] = pkg.Identifier
	}

	// Sanity: too many packages → MANUALLY_CANCELLED.
	if len(packageIDs) > maxPackages {
		logger.Warn("too many packages, cancelling", "count", len(packageIDs))
		return h.updateQueryState(ctx, msg, tenant.QueryStateManuallyCancelled, int64(sq.CfdiQty), packageIDs)
	}

	// Sanity: too many CFDIs for non-metadata → MANUALLY_CANCELLED.
	if msg.RequestType != tenant.RequestTypeMetadata && sq.CfdiQty > maxCFDIQtyInQuery {
		logger.Warn("too many CFDIs, cancelling", "count", sq.CfdiQty)
		return h.updateQueryState(ctx, msg, tenant.QueryStateManuallyCancelled, int64(sq.CfdiQty), packageIDs)
	}

	// Update to TO_DOWNLOAD and publish download event.
	if err := h.updateQueryState(ctx, msg, tenant.QueryStateToDownload, int64(sq.CfdiQty), packageIDs); err != nil {
		return err
	}

	h.Bus.Publish(event.EventTypeSATWSQueryDownloadReady, event.QueryVerifyEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: msg.CompanyIdentifier,
		QueryIdentifier:   msg.QueryIdentifier,
		DownloadType:      msg.DownloadType,
		RequestType:       msg.RequestType,
		Start:             msg.Start,
		End:               msg.End,
		State:             tenant.QueryStateToDownload,
		Name:              msg.Name,
		SentDate:          msg.SentDate,
		IsManual:          msg.IsManual,
		WID:               msg.WID,
		CID:               msg.CID,
		Packages:          packageIDs,
		CfdisQty:          int64(sq.CfdiQty),
	})

	logger.Warn("query ready to download", "packages", len(packageIDs), "cfdis", sq.CfdiQty)
	return nil
}

// handleError processes ERROR/REJECTED/EXPIRED verify statuses.
func (h *VerifyQuery) handleError(ctx context.Context, msg VerifyQueryMsg, sq *sat.Query, logger *slog.Logger) error {
	switch {
	case sq.StatusCode == verifyStatusCodeInfoNotFound:
		logger.Info("information not found")
		return h.updateQueryState(ctx, msg, tenant.QueryStateInformationNotFound, 0, nil)

	case sq.StatusCode == verifyStatusCodeMaxLimit:
		// SAT uses 5002 both for "volume too large" and for some reject/throttle cases.
		// When NumeroCFDIs is zero, treat as internal/throttle — not ERROR_TOO_BIG (no split).
		resolvedState := tenant.QueryStateErrorTooBig
		if sq.CfdiQty == 0 {
			resolvedState = tenant.QueryStateErrorSATWSInternal
		}
		// #region agent log
		agentDebugLog("H1", "verify_query.go:handleError:5002", "codigo 5002 resolution", map[string]any{
			"cfdi_qty":       sq.CfdiQty,
			"resolved_state": resolvedState,
			"mensaje":        sq.Message,
			"cod_estatus":    sq.CodEstatus,
			"query_status":   int(sq.QueryStatus),
			"request_type":   msg.RequestType,
			"download_type":  msg.DownloadType,
		})
		// #endregion

		if sq.CfdiQty == 0 {
			logger.Warn("SAT 5002 with zero CFDIs — storing ERROR_SAT_WS_INTERNAL (not ERROR_TOO_BIG)",
				"mensaje", sq.Message,
				"cod_estatus", sq.CodEstatus,
				"request_type", msg.RequestType,
			)
			return h.updateQueryState(ctx, msg, tenant.QueryStateErrorSATWSInternal, 0, nil)
		}

		logger.Warn("SAT 5002 with non-zero CFDIs — ERROR_TOO_BIG (split path for metadata)",
			"cfdi_qty", sq.CfdiQty,
			"mensaje", sq.Message,
			"request_type", msg.RequestType,
		)
		// #region agent log
		agentDebugLog("H2", "verify_query.go:handleError:5002", "volume split path", map[string]any{
			"cfdi_qty": sq.CfdiQty, "request_type": msg.RequestType,
		})
		// #endregion
		state := tenant.QueryStateErrorTooBig
		if err := h.updateQueryState(ctx, msg, state, 0, nil); err != nil {
			return err
		}
		// For METADATA queries, publish SAT_SPLIT_NEEDED.
		if msg.RequestType == tenant.RequestTypeMetadata {
			h.Bus.Publish(event.EventTypeSATSplitNeeded, event.SQSCompany{
				SQSBase:           event.NewSQSBase(),
				CompanyIdentifier: msg.CompanyIdentifier,
			})
		}
		return nil

	default:
		logger.Warn("query error", "status_code", sq.StatusCode, "message", sq.Message)
		return h.updateQueryState(ctx, msg, tenant.QueryStateError, 0, nil)
	}
}

// publishVerifyRetry enqueues another verify at executeAt.
func (h *VerifyQuery) publishVerifyRetry(msg VerifyQueryMsg, executeAt time.Time) error {
	delayAt := executeAt.UTC()
	h.Bus.Publish(event.EventTypeSATWSQueryVerifyNeeded, event.QueryVerifyEvent{
		SQSBase: event.SQSBase{
			Identifier: msg.Identifier,
			ExecuteAt:  &delayAt,
		},
		CompanyIdentifier: msg.CompanyIdentifier,
		QueryIdentifier:   msg.QueryIdentifier,
		DownloadType:      msg.DownloadType,
		RequestType:       msg.RequestType,
		Start:             msg.Start,
		End:               msg.End,
		State:             msg.State,
		Name:              msg.Name,
		SentDate:          msg.SentDate,
		IsManual:          msg.IsManual,
		WID:               msg.WID,
		CID:               msg.CID,
	})
	return nil
}

// retryOrTimeout schedules another verify with incremental backoff until
// WSMaxWaitingMinutes elapses from sent_date, then marks TIME_LIMIT_REACHED.
func (h *VerifyQuery) retryOrTimeout(msg VerifyQueryMsg, logger *slog.Logger) error {
	now := time.Now().UTC()
	elapsed := now.Sub(msg.SentDate)

	maxWait := h.Cfg.WSMaxWaitingMinutes
	if maxWait <= 0 {
		maxWait = defaultWSMaxWait
	}

	if elapsed >= maxWait {
		logger.Warn("time limit reached", "elapsed", elapsed, "max_wait", maxWait)
		ctx := context.Background()
		return h.updateQueryState(ctx, msg, tenant.QueryStateTimeLimitReached, 0, nil)
	}

	delay := verifyBackoffDelay(elapsed)
	if remain := maxWait - elapsed; delay > remain {
		delay = remain
	}
	if delay <= 0 {
		logger.Warn("time limit reached (zero delay)", "elapsed", elapsed, "max_wait", maxWait)
		ctx := context.Background()
		return h.updateQueryState(ctx, msg, tenant.QueryStateTimeLimitReached, 0, nil)
	}

	return h.publishVerifyRetry(msg, now.Add(delay))
}

// enrichSentDateIfMissing loads sent_date from sat_query when the queue payload omits it (legacy).
func (h *VerifyQuery) enrichSentDateIfMissing(ctx context.Context, msg *VerifyQueryMsg) error {
	if !msg.SentDate.IsZero() {
		return nil
	}
	conn, err := h.DB.TenantConn(ctx, msg.CompanyIdentifier, true)
	if err != nil {
		return fmt.Errorf("tenant conn for sent_date: %w", err)
	}
	defer conn.Close()

	var row tenant.SATQuery
	if err := conn.NewSelect().
		Model(&row).
		Column("sent_date").
		Where("identifier = ?", msg.QueryIdentifier).
		Scan(ctx); err != nil {
		return fmt.Errorf("load sat_query sent_date: %w", err)
	}
	if row.SentDate != nil {
		msg.SentDate = *row.SentDate
	}
	return nil
}

// updateQueryState updates the sat_query row with a new state and optional cfdis_qty/packages.
func (h *VerifyQuery) updateQueryState(ctx context.Context, msg VerifyQueryMsg, state string, cfdisQty int64, packages []string) error {
	conn, err := h.DB.TenantConn(ctx, msg.CompanyIdentifier, false)
	if err != nil {
		return fmt.Errorf("tenant conn for update: %w", err)
	}
	defer conn.Close()

	now := time.Now().UTC()
	q := conn.NewUpdate().
		Model((*tenant.SATQuery)(nil)).
		Set("state = ?", state).
		Set("updated_at = ?", now).
		Where("identifier = ?", msg.QueryIdentifier)

	if cfdisQty > 0 {
		q = q.Set("cfdis_qty = ?", cfdisQty)
	}

	if len(packages) > 0 {
		pkgJSON, _ := json.Marshal(packages)
		q = q.Set("packages = ?", string(pkgJSON))
	}

	if _, err := q.Exec(ctx); err != nil {
		return fmt.Errorf("update sat_query state to %s: %w", state, err)
	}

	return nil
}
