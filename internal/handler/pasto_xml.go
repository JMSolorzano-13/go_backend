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

type PastoXML struct {
	cfg      *config.Config
	database *db.Database
}

func NewPastoXML(cfg *config.Config, database *db.Database) *PastoXML {
	return &PastoXML{cfg: cfg, database: database}
}

// POST /api/Pasto/XML — XML upload notification from ADD.
// Matches routers/pasto/xml.py::xml_webhook.
func (h *PastoXML) XMLWebhook(w http.ResponseWriter, r *http.Request) {
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
	webhookErr, pastoBody, hdrs := parsePastoWebhook(body, headers, "xml_webhook")

	requestIdentifier := headerStr(hdrs, "request_identifier")
	companyIdentifier := headerStr(hdrs, "company_identifier")

	slog.Debug("pasto_xml: webhook received", "company", companyIdentifier, "request", requestIdentifier, "error", webhookErr)

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	tenantConn, err := database.TenantConn(ctx, companyIdentifier, false)
	if err != nil {
		slog.Error("pasto_xml: tenant session failed", "company", companyIdentifier, "err", err)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	defer tenantConn.Close()

	var addReq tenant.ADDSyncRequest
	if err := tenantConn.NewSelect().Model(&addReq).Where("identifier = ?", requestIdentifier).Limit(1).Scan(ctx); err != nil {
		slog.Warn("pasto_xml: add_sync_request not found", "request", requestIdentifier)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if pastoBody == nil {
		addReq.State = tenant.ADDSyncStateError
		_, _ = tenantConn.NewUpdate().Model(&addReq).Column("state").Where("identifier = ?", addReq.Identifier).Exec(ctx)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	errorRows := int64(0)
	if v, ok := pastoBody["ErrorRows"].(float64); ok {
		errorRows = int64(v)
	}
	addReq.XMLsToSendPending = errorRows
	if errorRows > 0 {
		addReq.State = tenant.ADDSyncStateError
	}
	_, _ = tenantConn.NewUpdate().Model(&addReq).Column("xmls_to_send_pending", "state").Where("identifier = ?", addReq.Identifier).Exec(ctx)

	// Update add_exists = true for successfully sent UUIDs
	reports, _ := pastoBody["Reports"].([]interface{})
	var successUUIDs []string
	for _, rep := range reports {
		repMap, ok := rep.(map[string]interface{})
		if !ok {
			continue
		}
		success, _ := repMap["Success"].(bool)
		if success {
			if uid, ok := repMap["Uuid"].(string); ok && uid != "" {
				successUUIDs = append(successUUIDs, uid)
			}
		}
	}

	if len(successUUIDs) > 0 {
		updateCFDIAddExists(ctx, tenantConn, successUUIDs, true)
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// updateCFDIAddExists bulk-updates add_exists on the cfdi table.
func updateCFDIAddExists(ctx context.Context, tenantDB bun.IDB, uuids []string, exists bool) {
	if len(uuids) == 0 {
		return
	}
	_, err := tenantDB.NewUpdate().
		TableExpr("cfdi").
		Set("add_exists = ?", exists).
		Where(`"UUID" IN (?)`, bun.In(uuids)).
		Exec(ctx)
	if err != nil {
		slog.Error("pasto_xml: update add_exists failed", "err", err, "count", len(uuids))
	} else {
		slog.Info(fmt.Sprintf("pasto_xml: updated add_exists=%v for %d CFDIs", exists, len(uuids)))
	}
}
