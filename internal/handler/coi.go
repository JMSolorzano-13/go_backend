package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	dtdomain "github.com/siigofiscal/go_backend/internal/domain/datetime"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

const (
	coiPrefix         = "coi"
	coiMetadataSuffix = "metadata.csv"
	coiDataSuffix     = "data.zip"
	coiCancelSuffix   = "cancel.csv"
)

type COI struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
	files    port.FileStorage
}

func NewCOI(cfg *config.Config, database *db.Database, bus *event.Bus, files port.FileStorage) *COI {
	return &COI{cfg: cfg, database: database, bus: bus, files: files}
}

var coiMeta = crud.ModelMeta{
	DefaultOrderBy: "created_at DESC",
}

// POST /api/COI/search
func (h *COI) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var rawBody map[string]interface{}
	if err := json.Unmarshal(raw, &rawBody); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	tenantConn, err := database.TenantConn(ctx, cid, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer tenantConn.Close()

	params := crud.ParseSearchBody(rawBody)
	result, err := crud.Search[tenant.ADDSyncRequest](ctx, tenantConn, params, coiMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

// GET /api/COI/{company_identifier}/{identifier}
func (h *COI) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	companyIdentifier := r.PathValue("company_identifier")
	identifier := r.PathValue("identifier")

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	tenantConn, err := database.TenantConn(ctx, companyIdentifier, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer tenantConn.Close()

	var req tenant.ADDSyncRequest
	if err := tenantConn.NewSelect().Model(&req).Where("identifier = ?", identifier).Limit(1).Scan(ctx); err != nil {
		response.NotFound(w, "ADDSyncRequest not found")
		return
	}

	res := map[string]interface{}{
		"identifier": req.Identifier,
		"start":      req.Start.Format("2006-01-02T15:04:05"),
		"end":        req.End.Format("2006-01-02T15:04:05"),
		"state":      req.State,
	}

	if req.State == tenant.ADDSyncStateSent {
		exp := h.cfg.ADDS3ExpirationDelta
		xmlURL, err := h.files.PresignGet(ctx, h.cfg.S3ADD, coiPath(companyIdentifier, identifier, coiDataSuffix), exp)
		if err != nil {
			slog.Error("coi: presign xml_url failed", "err", err)
		}
		cancelURL, err := h.files.PresignGet(ctx, h.cfg.S3ADD, coiPath(companyIdentifier, identifier, coiCancelSuffix), exp)
		if err != nil {
			slog.Error("coi: presign cancel_url failed", "err", err)
		}
		resultURL, err := h.files.PresignPut(ctx, h.cfg.S3ADD, coiPath(companyIdentifier, identifier, coiMetadataSuffix), exp)
		if err != nil {
			slog.Error("coi: presign result_url failed", "err", err)
		}
		res["xml_url"] = xmlURL
		res["to_cancel_url"] = cancelURL
		res["result_url"] = resultURL
	}

	response.WriteJSON(w, http.StatusOK, res)
}

// POST /api/COI/{company_identifier}/{identifier}/notify
func (h *COI) NotifyMetadataUploaded(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	companyIdentifier := r.PathValue("company_identifier")
	identifier := r.PathValue("identifier")

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body struct {
		IsResult bool `json:"is_result"`
	}
	_ = json.Unmarshal(raw, &body)

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	tenantConn, err := database.TenantConn(ctx, companyIdentifier, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer tenantConn.Close()

	var req tenant.ADDSyncRequest
	if err := tenantConn.NewSelect().Model(&req).Where("identifier = ?", identifier).Limit(1).Scan(ctx); err != nil {
		response.NotFound(w, "ADDSyncRequest not found")
		return
	}

	req.XMLsToSendPending = 0
	req.CfdisToCancelPending = 0
	_, _ = tenantConn.NewUpdate().Model(&req).Column("xmls_to_send_pending", "cfdis_to_cancel_pending").Where("identifier = ?", identifier).Exec(ctx)

	launchSync := true
	if body.IsResult {
		launchSync = false
	}

	h.bus.Publish(event.EventTypeCOIMetadataUploaded, event.COIMetadataUploadedEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: companyIdentifier,
		RequestIdentifier: identifier,
		LaunchSync:        launchSync,
	})

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"identifier": req.Identifier,
		"is_result":  body.IsResult,
	})
}

// POST /api/COI/{company_identifier} — create new COI sync request.
func (h *COI) NewSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	companyIdentifier := r.PathValue("company_identifier")

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	_ = json.Unmarshal(raw, &body)

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	// Set coi_enabled flag on company.
	var company control.Company
	if err := database.Primary.NewSelect().Model(&company).Where("identifier = ?", companyIdentifier).Limit(1).Scan(ctx); err != nil {
		response.NotFound(w, "company not found")
		return
	}
	updateCompanyCOIFlag(ctx, database, companyIdentifier)

	tenantConn, err := database.TenantConn(ctx, companyIdentifier, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer tenantConn.Close()

	start := dtdomain.LastXFiscalYearsStart(5)
	end := dtdomain.MXCalendarDate(time.Now())
	if body.Start != "" {
		if t, err := time.Parse(time.RFC3339, body.Start); err == nil {
			start = t
		} else if t, err := time.Parse("2006-01-02", body.Start); err == nil {
			start = t
		}
	}
	if body.End != "" {
		if t, err := time.Parse(time.RFC3339, body.End); err == nil {
			end = t
		} else if t, err := time.Parse("2006-01-02", body.End); err == nil {
			end = t
		}
	}

	req := &tenant.ADDSyncRequest{
		Identifier:        uuid.NewString(),
		CreatedAt:         time.Now().UTC(),
		Start:             start,
		End:               end,
		ManuallyTriggered: true,
		State:             tenant.ADDSyncStateDraft,
	}
	if _, err := tenantConn.NewInsert().Model(req).Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("create sync request: %v", err))
		return
	}

	uploadURL, err := h.files.PresignPut(ctx, h.cfg.S3ADD, coiPath(companyIdentifier, req.Identifier, coiMetadataSuffix), h.cfg.ADDS3ExpirationDelta)
	if err != nil {
		slog.Error("coi: presign upload_url failed", "err", err)
		response.InternalError(w, "could not generate presigned URL")
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"identifier":          req.Identifier,
		"start":               req.Start.Format("2006-01-02T15:04:05"),
		"end":                 req.End.Format("2006-01-02T15:04:05"),
		"state":               req.State,
		"url_upload_metadata": uploadURL,
	})
}

func coiPath(companyIdentifier, requestIdentifier, resource string) string {
	return fmt.Sprintf("%s/%s/%s/%s", coiPrefix, companyIdentifier, requestIdentifier, resource)
}

func updateCompanyCOIFlag(ctx context.Context, database *db.Database, companyIdentifier string) {
	var company control.Company
	if err := database.Primary.NewSelect().Model(&company).Where("identifier = ?", companyIdentifier).Limit(1).Scan(ctx); err != nil {
		return
	}
	var data map[string]interface{}
	if len(company.Data) > 0 {
		_ = json.Unmarshal(company.Data, &data)
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	data["coi_enabled"] = true
	updated, _ := json.Marshal(data)
	_, _ = database.Primary.NewUpdate().
		Model((*control.Company)(nil)).
		Set("data = ?", string(updated)).
		Where("identifier = ?", companyIdentifier).
		Exec(ctx)
}
