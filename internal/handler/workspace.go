package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Workspace struct {
	cfg      *config.Config
	database *db.Database
}

func NewWorkspace(cfg *config.Config, database *db.Database) *Workspace {
	return &Workspace{cfg: cfg, database: database}
}

var workspaceMeta = crud.ModelMeta{
	DefaultOrderBy: "id ASC",
	FuzzyFields:    []string{"name"},
}

// Search handles POST /api/Workspace/search — no auth required.
func (h *Workspace) Search(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var rawBody map[string]interface{}
	if err := json.Unmarshal(body, &rawBody); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	params := crud.ParseSearchBody(rawBody)
	result, err := crud.Search[control.Workspace](r.Context(), h.database.Replica, params, workspaceMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, result)
}

// Create handles POST /api/Workspace/ — creates a workspace for the current user.
//
// Python source: WorkspaceController.create() sets owner_id from context user,
// strips license from input, creates the workspace, then initialises the license
// (via WorkspaceController.init_license) and optionally notifies Odoo/Stripe.
// Odoo/Stripe notifications are deferred to Phases 10-11.
func (h *Workspace) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	// Strip restricted fields matching Python WorkspaceController.create.
	delete(data, "license")
	delete(data, "id")
	delete(data, "created_at")
	delete(data, "updated_at")

	identifier := crud.NewIdentifier()
	defaultLicense := h.defaultLicense()
	licenseJSON, _ := json.Marshal(defaultLicense)
	now := time.Now().UTC()
	validUntil := now.Add(h.cfg.DefaultLicenseLifetime)
	stripeStatus := "trial"

	workspace := &control.Workspace{
		Identifier:   identifier,
		OwnerID:      &user.ID,
		License:      licenseJSON,
		ValidUntil:   &validUntil,
		StripeStatus: &stripeStatus,
	}

	if name, ok := data["name"].(string); ok {
		workspace.Name = &name
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	if _, err := database.Primary.NewInsert().Model(workspace).Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("insert workspace: %v", err))
		return
	}

	result := crud.SerializeOne(*workspace)
	response.WriteJSON(w, http.StatusOK, result)
}

// Update handles PUT /api/Workspace/ — updates workspace fields.
//
// Body: {ids: [str], values: {field: value}} — matches Python common.update contract
func (h *Workspace) Update(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var req struct {
		IDs    []string               `json:"ids"`
		Values map[string]interface{} `json:"values"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.IDs) == 0 {
		response.BadRequest(w, "ids is required")
		return
	}

	// Block license field — use dedicated license endpoints.
	delete(req.Values, "license")
	if err := crud.ValidateUpdateData(req.Values); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	records, err := crud.Update[control.Workspace](ctx, database.Primary, req.IDs, req.Values)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("update workspace: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, crud.Serialize(records))
}

// Delete handles DELETE /api/Workspace/ — deletes workspace records.
//
// Body: {ids: [str]} — matches Python common.delete contract
func (h *Workspace) Delete(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.IDs) == 0 {
		response.BadRequest(w, "ids is required")
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	deletedIDs, err := crud.Delete[control.Workspace](ctx, database.Primary, req.IDs)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("delete workspace: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"deleted": deletedIDs,
	})
}

// GetLicense handles GET /api/Workspace/{workspace_id}/license/{key} — admin only.
//
// Python: workspace.license.get(key) — returns the value at the given license key.
func (h *Workspace) GetLicense(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspace_id")
	key := r.PathValue("key")
	if workspaceID == "" || key == "" {
		response.BadRequest(w, "workspace_identifier and key are required")
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var workspace control.Workspace
	if err := database.Replica.NewSelect().Model(&workspace).
		Where("identifier = ?", workspaceID).
		Scan(ctx); err != nil {
		response.NotFound(w, fmt.Sprintf("workspace %s not found", workspaceID))
		return
	}

	var licenseMap map[string]interface{}
	if err := json.Unmarshal(workspace.License, &licenseMap); err != nil {
		response.InternalError(w, "invalid license JSON")
		return
	}

	val, exists := licenseMap[key]
	if !exists {
		response.WriteJSON(w, http.StatusOK, nil)
		return
	}

	response.WriteJSON(w, http.StatusOK, val)
}

// SetLicense handles PUT /api/Workspace/{workspace_id}/license/{key} — admin only.
//
// Python: workspace.license[key] = body["value"] — sets a license key value.
func (h *Workspace) SetLicense(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspace_id")
	key := r.PathValue("key")
	if workspaceID == "" || key == "" {
		response.BadRequest(w, "workspace_identifier and key are required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var req struct {
		Value interface{} `json:"value"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var workspace control.Workspace
	if err := database.Primary.NewSelect().Model(&workspace).
		Where("identifier = ?", workspaceID).
		Scan(ctx); err != nil {
		response.NotFound(w, fmt.Sprintf("workspace %s not found", workspaceID))
		return
	}

	var licenseMap map[string]interface{}
	if err := json.Unmarshal(workspace.License, &licenseMap); err != nil {
		licenseMap = make(map[string]interface{})
	}

	licenseMap[key] = req.Value
	newLicenseJSON, err := json.Marshal(licenseMap)
	if err != nil {
		response.InternalError(w, "marshal license")
		return
	}

	workspace.License = newLicenseJSON
	if _, err := database.Primary.NewUpdate().
		Model(&workspace).
		Column("license").
		WherePK().
		Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("update license: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"key":   key,
		"value": req.Value,
	})
}

// defaultLicense returns the default trial license matching Python's WorkspaceController.default_license.
func (h *Workspace) defaultLicense() map[string]interface{} {
	now := time.Now().UTC()
	return map[string]interface{}{
		"date_start": now.Format(crud.APITimestampFormat),
		"date_end":   now.Add(h.cfg.DefaultLicenseLifetime).Format(crud.APITimestampFormat),
		"details": map[string]interface{}{
			"max_companies":       1,
			"max_emails_enroll":   1,
		},
	}
}
