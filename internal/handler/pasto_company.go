package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	pastoclient "github.com/siigofiscal/go_backend/internal/infra/pasto"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
	"github.com/uptrace/bun"
)

type PastoCompany struct {
	cfg      *config.Config
	database *db.Database
	pasto    *pastoclient.Client
}

func NewPastoCompany(cfg *config.Config, database *db.Database) *PastoCompany {
	pastoClient := pastoclient.NewClient(cfg.PastoURL, cfg.PastoOCPKey, cfg.PastoRequestTimeout)
	return &PastoCompany{cfg: cfg, database: database, pasto: pastoClient}
}

// pastoCompanyRowsFromBody normalizes the inner ADD company webhook JSON to a list of row maps.
func pastoCompanyRowsFromBody(rawBody interface{}) []interface{} {
	switch v := rawBody.(type) {
	case []interface{}:
		return v
	case map[string]interface{}:
		if arr, ok := v["companies"].([]interface{}); ok {
			return arr
		}
		if arr, ok := v["data"].([]interface{}); ok {
			return arr
		}
	}
	return nil
}

var pastoCompanyMeta = crud.ModelMeta{
	DefaultOrderBy: "created_at DESC",
}

// POST /api/Pasto/Company/ — company data webhook from ADD.
// Matches routers/pasto/company.py::company_webhook.
func (h *PastoCompany) CompanyWebhook(w http.ResponseWriter, r *http.Request) {
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
	webhookErr, rawBody, hdrs := parsePastoWebhookRaw(body, headers, "company_webhook")
	if webhookErr {
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	// Python company_webhook: if not body: raise BadRequestError("Body is empty")
	if rawBody == nil {
		response.BadRequest(w, "Body is empty")
		return
	}
	companiesData := pastoCompanyRowsFromBody(rawBody)
	if len(companiesData) == 0 {
		response.BadRequest(w, "Body is empty")
		return
	}

	workerID := headerStr(hdrs, "worker_id")
	workspaceIdentifier := headerStr(hdrs, "workspace_identifier")

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var workspace control.Workspace
	if err := database.Primary.NewSelect().
		Model(&workspace).
		Where("identifier = ?", workspaceIdentifier).
		Limit(1).
		Scan(ctx); err != nil {
		slog.Error("pasto_company: workspace not found", "workspace", workspaceIdentifier)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	toDelete, toCreate, toUpdate, err := syncPastoCompaniesImpl(ctx, database, workspaceIdentifier, workerID, companiesData)
	if err != nil {
		slog.Error("pasto_company: sync failed", "err", err)
	}

	slog.Info("pasto_company: synced", "workspace", workspaceIdentifier,
		"to_delete", toDelete, "to_create", toCreate, "to_update", toUpdate)
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/Pasto/Company/search — search Pasto companies.
// Matches routers/pasto/company.py::search.
func (h *PastoCompany) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	params := crud.ParseSearchBody(rawBody)
	result, err := crud.Search[control.PastoCompany](ctx, database.Replica, params, pastoCompanyMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

// POST /api/Pasto/Company/request_new — request ADD worker to push company list.
// Matches routers/pasto/company.py::request_new.
func (h *PastoCompany) RequestNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body struct {
		WorkspaceIdentifier string `json:"workspace_identifier"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var workspace control.Workspace
	if err := database.Primary.NewSelect().
		Model(&workspace).
		Where("identifier = ?", body.WorkspaceIdentifier).
		Limit(1).
		Scan(ctx); err != nil {
		response.NotFound(w, "workspace not found")
		return
	}

	workerID := ""
	workerToken := ""
	if workspace.PastoWorkerID != nil {
		workerID = *workspace.PastoWorkerID
	}
	if workspace.PastoWorkerToken != nil {
		workerToken = *workspace.PastoWorkerToken
	}

	if h.cfg.LocalInfra {
		slog.Info("pasto_company: mock mode — skipping request_new Pasto API call", "workspace", body.WorkspaceIdentifier)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if err := h.pasto.RequestCompanies(
		workerToken,
		body.WorkspaceIdentifier,
		workerID,
		h.cfg.SelfEndpoint,
		h.cfg.ADDCompaniesWebhook,
	); err != nil {
		slog.Error("pasto_company: request_companies failed", "err", err)
		response.InternalError(w, "request companies failed")
		return
	}

	slog.Info("pasto_company: companies requested", "workspace", body.WorkspaceIdentifier)
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// syncPastoCompaniesImpl upserts PastoCompany records from ADD webhook payload.
// Matches CompanyCreator.create in chalicelib/new/pasto/company_creator.py.
func syncPastoCompaniesImpl(ctx context.Context, database *db.Database, workspaceIdentifier, _ string, companiesData []interface{}) (toDelete, toCreate, toUpdate int, err error) {
	type pastoEntry struct {
		ID     string
		Name   string
		Alias  string
		RFC    string
		BDD    string
		System string
	}

	incoming := make(map[string]pastoEntry)
	for _, raw := range companiesData {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["GuidAdd"].(string)
		if id == "" {
			id, _ = m["Id"].(string)
		}
		idField, _ := m["Id"].(string)
		name, _ := m["NombreEmpresa"].(string)
		alias, _ := m["Alias"].(string)
		rfc, _ := m["RFC"].(string)
		bdd, _ := m["BDD"].(string)
		system, _ := m["Sistema"].(string)
		// Match Python CompanyCreator._build_pasto_companies: all fields required.
		if id == "" || idField == "" || name == "" || alias == "" || rfc == "" || bdd == "" || system == "" {
			continue
		}
		id = strings.ToLower(id)
		incoming[id] = pastoEntry{ID: id, Name: name, Alias: alias, RFC: rfc, BDD: bdd, System: system}
	}

	var existing []control.PastoCompany
	if err := database.Primary.NewSelect().
		Model(&existing).
		Where("workspace_identifier = ?", workspaceIdentifier).
		Scan(ctx); err != nil {
		return 0, 0, 0, err
	}

	existingMap := make(map[string]*control.PastoCompany)
	for i := range existing {
		id := strings.ToLower(existing[i].PastoCompanyID)
		existingMap[id] = &existing[i]
	}

	var toDeleteIDs []string
	for id := range existingMap {
		if _, ok := incoming[id]; !ok {
			toDeleteIDs = append(toDeleteIDs, id)
		}
	}
	if len(toDeleteIDs) > 0 {
		_, err := database.Primary.NewDelete().
			Model((*control.PastoCompany)(nil)).
			Where("workspace_identifier = ? AND pasto_company_id IN (?)", workspaceIdentifier, bun.In(toDeleteIDs)).
			Exec(ctx)
		if err != nil {
			slog.Error("syncPastoCompanies: delete failed", "err", err)
		}
		toDelete = len(toDeleteIDs)
	}

	now := time.Now().UTC()
	for id, entry := range incoming {
		if existing, ok := existingMap[id]; ok {
			existing.Name = entry.Name
			existing.Alias = entry.Alias
			existing.RFC = entry.RFC
			existing.BDD = &entry.BDD
			existing.System = &entry.System
			_, err := database.Primary.NewUpdate().
				Model(existing).
				Column("name", "alias", "rfc", "bdd", "system", "updated_at").
				Where("pasto_company_id = ?", id).
				Exec(ctx)
			if err != nil {
				slog.Error("syncPastoCompanies: update failed", "id", id, "err", err)
			}
			toUpdate++
		} else {
			bdd := entry.BDD
			sys := entry.System
			newCompany := &control.PastoCompany{
				PastoCompanyID:      id,
				WorkspaceIdentifier: workspaceIdentifier,
				Name:                entry.Name,
				Alias:               entry.Alias,
				RFC:                 entry.RFC,
				BDD:                 &bdd,
				System:              &sys,
				CreatedAt:           now,
			}
			_, err := database.Primary.NewInsert().Model(newCompany).Exec(ctx)
			if err != nil {
				slog.Error("syncPastoCompanies: insert failed", "id", id, "err", err)
			}
			toCreate++
		}
	}
	return toDelete, toCreate, toUpdate, nil
}
