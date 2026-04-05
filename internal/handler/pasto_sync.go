package handler

import (
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
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

type PastoSync struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
}

func NewPastoSync(cfg *config.Config, database *db.Database, bus *event.Bus) *PastoSync {
	return &PastoSync{cfg: cfg, database: database, bus: bus}
}

var addSyncMeta = crud.ModelMeta{
	DefaultOrderBy: "created_at DESC",
}

// POST /api/Pasto/Sync/ — create a new ADD sync request.
// Matches routers/pasto/sync.py::create_sync_request.
func (h *PastoSync) CreateSyncRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body struct {
		CompanyIdentifier string `json:"company_identifier"`
		Start             string `json:"start"`
		End               string `json:"end"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}
	if body.CompanyIdentifier == "" {
		body.CompanyIdentifier = cid
	}

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var company control.Company
	if err := database.Primary.NewSelect().
		Model(&company).
		Relation("Workspace").
		Where("c.identifier = ?", body.CompanyIdentifier).
		Limit(1).
		Scan(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("company not found: %v", err))
		return
	}

	pastoToken := ""
	pastoCompanyIdentifier := ""
	if company.Workspace != nil && company.Workspace.PastoWorkerToken != nil {
		pastoToken = *company.Workspace.PastoWorkerToken
	}
	if company.PastoCompanyIdentifier != nil {
		pastoCompanyIdentifier = *company.PastoCompanyIdentifier
	}

	start, end := dtdomain.ADDDefaultSyncWindow()
	if body.Start != "" {
		if t, ok := parseADDSyncDate(body.Start); ok {
			start = t
		}
	}
	if body.End != "" {
		if t, ok := parseADDSyncDate(body.End); ok {
			end = t
		}
	}

	tenantConn, err := database.TenantConn(ctx, body.CompanyIdentifier, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer tenantConn.Close()

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

	h.bus.Publish(event.EventTypeADDSyncRequestCreated, event.ADDSyncRequestCreatedEvent{
		SQSBase:                event.NewSQSBase(),
		CompanyIdentifier:      body.CompanyIdentifier,
		RequestIdentifier:      req.Identifier,
		PastoCompanyIdentifier: pastoCompanyIdentifier,
		PastoWorkerToken:       pastoToken,
		Start:                  start.Format("2006-01-02"),
		End:                    end.Format("2006-01-02"),
	})

	slog.Info("pasto_sync: sync request created", "company", body.CompanyIdentifier, "request", req.Identifier)
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/Pasto/Sync/enable_auto_sync — enable/disable ADD auto sync.
// Matches routers/pasto/sync.py::enable_auto_sync.
func (h *PastoSync) EnableAutoSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body struct {
		CompanyIdentifier string `json:"company_identifier"`
		AddAutoState      bool   `json:"add_auto_state"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	var company control.Company
	if err := database.Primary.NewSelect().
		Model(&company).
		Relation("Workspace").
		Where("c.identifier = ?", body.CompanyIdentifier).
		Limit(1).
		Scan(ctx); err != nil {
		response.NotFound(w, "company not found")
		return
	}

	if company.Workspace == nil || company.Workspace.ADDPermission == nil || !*company.Workspace.ADDPermission {
		response.WriteJSON(w, http.StatusForbidden, map[string]string{
			"message": "You don't have permission to add auto sync",
		})
		return
	}

	_, err = database.Primary.NewUpdate().
		Model((*control.Company)(nil)).
		Set("add_auto_sync = ?", body.AddAutoState).
		Where("identifier = ?", body.CompanyIdentifier).
		Exec(ctx)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("update company: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"add_auto_sync": body.AddAutoState})
}

// POST /api/Pasto/Sync/create_metadata_sync_request — trigger ADD metadata sync.
// Matches routers/pasto/sync.py::create_metadata_sync_request.
func (h *PastoSync) CreateMetadataSyncRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body struct {
		CompanyIdentifier string `json:"company_identifier"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	var company control.Company
	if err := database.Primary.NewSelect().
		Model(&company).
		Relation("Workspace").
		Where("c.identifier = ?", body.CompanyIdentifier).
		Limit(1).
		Scan(ctx); err != nil {
		response.NotFound(w, "company not found")
		return
	}

	if company.Workspace == nil || company.Workspace.ADDPermission == nil || !*company.Workspace.ADDPermission {
		response.WriteJSON(w, http.StatusForbidden, map[string]interface{}{
			"status":  "error",
			"message": "No tiene permisos para sincronizar",
		})
		return
	}

	h.bus.Publish(event.EventTypeADDMetadataRequested, event.SQSCompanyManual{
		SQSCompany:        event.SQSCompany{SQSBase: event.NewSQSBase(), CompanyIdentifier: body.CompanyIdentifier},
		ManuallyTriggered: true,
	})

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/Pasto/Sync/search — search ADD sync requests.
// Matches routers/pasto/sync.py::search.
func (h *PastoSync) Search(w http.ResponseWriter, r *http.Request) {
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
	result, err := crud.Search[tenant.ADDSyncRequest](ctx, tenantConn, params, addSyncMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

// parseADDSyncDate accepts YYYY-MM-DD or RFC3339 (Python/json often sends full ISO datetimes).
func parseADDSyncDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}
