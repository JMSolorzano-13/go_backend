package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

type PastoCancel struct {
	cfg      *config.Config
	database *db.Database
}

func NewPastoCancel(cfg *config.Config, database *db.Database) *PastoCancel {
	return &PastoCancel{cfg: cfg, database: database}
}

// POST /api/Pasto/Cancel — cancellation notification from ADD.
// Matches routers/pasto/cancel.py::cancel_webhook.
func (h *PastoCancel) CancelWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	headers := extractRequestHeaders(r)
	webhookErr, pastoBody, hdrs := parsePastoWebhook(body, headers, "cancel_webhook")

	requestIdentifier := headerStr(hdrs, "request_identifier")
	companyIdentifier := headerStr(hdrs, "company_identifier")

	slog.Debug("pasto_cancel: webhook received", "company", companyIdentifier, "request", requestIdentifier, "error", webhookErr)

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	tenantConn, err := database.TenantConn(ctx, companyIdentifier, false)
	if err != nil {
		slog.Error("pasto_cancel: tenant session failed", "company", companyIdentifier, "err", err)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	defer tenantConn.Close()

	var addReq tenant.ADDSyncRequest
	if err := tenantConn.NewSelect().Model(&addReq).Where("identifier = ?", requestIdentifier).Limit(1).Scan(ctx); err != nil {
		slog.Warn("pasto_cancel: add_sync_request not found", "request", requestIdentifier)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if webhookErr || pastoBody == nil {
		addReq.State = tenant.ADDSyncStateError
		_, _ = tenantConn.NewUpdate().Model(&addReq).Column("state").Where("identifier = ?", addReq.Identifier).Exec(ctx)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	errorRows := int64(0)
	if v, ok := pastoBody["ErrorRows"].(float64); ok {
		errorRows = int64(v)
	}
	addReq.CfdisToCancelPending = errorRows
	if errorRows > 0 {
		addReq.State = tenant.ADDSyncStateError
	}
	_, _ = tenantConn.NewUpdate().Model(&addReq).Column("cfdis_to_cancel_pending", "state").Where("identifier = ?", addReq.Identifier).Exec(ctx)

	// Update add_cancel_date = FechaCancelacion for successfully cancelled UUIDs.
	reports, _ := pastoBody["Reports"].([]interface{})
	var cancelUUIDs []string
	for _, rep := range reports {
		repMap, ok := rep.(map[string]interface{})
		if !ok {
			continue
		}
		success, _ := repMap["Success"].(bool)
		if success {
			if uid, ok := repMap["Uuid"].(string); ok && uid != "" {
				cancelUUIDs = append(cancelUUIDs, uid)
			}
		}
	}

	if len(cancelUUIDs) > 0 {
		updateCFDICancelDateFromFechaCancelacion(ctx, tenantConn, cancelUUIDs)
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// updateCFDICancelDateFromFechaCancelacion sets add_cancel_date = "FechaCancelacion" for cancelled CFDIs.
// Matches MetadataUpdater.update_cancel_date_optimistic.
func updateCFDICancelDateFromFechaCancelacion(ctx context.Context, tenantDB bun.IDB, uuids []string) {
	if len(uuids) == 0 {
		return
	}
	_, err := tenantDB.NewUpdate().
		TableExpr("cfdi").
		Set(`"add_cancel_date" = "FechaCancelacion"`).
		Where(`"UUID" IN (?)`, bun.In(uuids)).
		Exec(ctx)
	if err != nil {
		slog.Error("pasto_cancel: update add_cancel_date failed", "err", err, "count", len(uuids))
	} else {
		slog.Info(fmt.Sprintf("pasto_cancel: updated add_cancel_date for %d CFDIs", len(uuids)))
	}
}
