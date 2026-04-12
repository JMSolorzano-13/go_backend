package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/company"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	s3keys "github.com/siigofiscal/go_backend/internal/infra/s3"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Company struct {
	cfg        *config.Config
	database   *db.Database
	bus        *event.Bus
	files      port.FileStorage
	certMirror port.FileStorage // optional: LocalStack S3 mirror for Python SAT worker when blob is Azurite
}

func NewCompany(cfg *config.Config, database *db.Database, bus *event.Bus, files, certMirror port.FileStorage) *Company {
	return &Company{cfg: cfg, database: database, bus: bus, files: files, certMirror: certMirror}
}

var companyMeta = crud.ModelMeta{
	DefaultOrderBy: "id ASC",
	FuzzyFields:    []string{"name", "rfc"},
	ActiveColumn:   "active",
	Relations:      []string{"Workspace", "Workspace.Owner"},
	TableAlias:     "c",
}

// Search handles POST /api/Company/search.
func (h *Company) Search(w http.ResponseWriter, r *http.Request) {
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
	result, err := crud.Search[control.Company](r.Context(), h.database.Primary, params, companyMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

// Create handles POST /api/Company/ — creates a company from FIEL certs.
func (h *Company) Create(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Cer                 string `json:"cer"`
		Key                 string `json:"key"`
		Pas                 string `json:"pas"`
		WorkspaceIdentifier string `json:"workspace_identifier"`
		WorkspaceID         int64  `json:"workspace_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	cerBytes, err := base64.StdEncoding.DecodeString(req.Cer)
	if err != nil {
		response.BadRequest(w, "invalid base64 for cer")
		return
	}
	keyBytes, err := base64.StdEncoding.DecodeString(req.Key)
	if err != nil {
		response.BadRequest(w, "invalid base64 for key")
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	newCompany, err := h.createFromCerts(ctx, database, user, req.WorkspaceIdentifier, req.WorkspaceID, cerBytes, keyBytes, req.Pas)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// Populate email lists
	populateCompanyEmails(newCompany, user.Email)
	if _, err := database.Primary.NewUpdate().Model(newCompany).
		Column("emails_to_send_efos", "emails_to_send_errors", "emails_to_send_canceled").
		WherePK().Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("update email lists: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, crud.SerializeOne(*newCompany))
}

// AdminCreate handles POST /api/Company/admin_create.
func (h *Company) AdminCreate(w http.ResponseWriter, r *http.Request) {
	adminUser, _ := auth.UserFromContext(r.Context())
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Cer    string `json:"cer"`
		Key    string `json:"key"`
		Pas    string `json:"pas"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	cerBytes, err := base64.StdEncoding.DecodeString(req.Cer)
	if err != nil {
		response.BadRequest(w, "invalid base64 for cer")
		return
	}
	keyBytes, err := base64.StdEncoding.DecodeString(req.Key)
	if err != nil {
		response.BadRequest(w, "invalid base64 for key")
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	// Validate certificate first
	cert, err := company.ValidateFIELCertificate(cerBytes, keyBytes, req.Pas)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// Get or create the target user
	targetUser, err := h.getOrCreateUserByEmail(ctx, database, req.UserID)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("get or create user: %v", err))
		return
	}

	// Check if company with same RFC already exists in user's workspace
	var existingCompany control.Company
	var workspace control.Workspace
	if err := database.Primary.NewSelect().Model(&workspace).
		Where("owner_id = ?", targetUser.ID).
		Limit(1).Scan(ctx); err != nil {
		response.InternalError(w, "workspace not found for user")
		return
	}

	err = database.Primary.NewSelect().Model(&existingCompany).
		Where("workspace_id = ?", workspace.ID).
		Where("rfc = ?", cert.RFC).
		Limit(1).Scan(ctx)
	if err == nil {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"company_identifier": existingCompany.Identifier,
		})
		return
	}

	// Create the company
	newCompany, err := h.createFromCerts(ctx, database, targetUser, workspace.Identifier, workspace.ID, cerBytes, keyBytes, req.Pas)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// Add permissions for the admin user
	opPerm := &control.Permission{
		Identifier: crud.NewIdentifier(),
		UserID:     adminUser.ID,
		CompanyID:  newCompany.ID,
		Role:       control.RoleOperator,
	}
	payrollPerm := &control.Permission{
		Identifier: crud.NewIdentifier(),
		UserID:     adminUser.ID,
		CompanyID:  newCompany.ID,
		Role:       control.RolePayroll,
	}
	if _, err := database.Primary.NewInsert().Model(opPerm).Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("create admin operator permission: %v", err))
		return
	}
	if _, err := database.Primary.NewInsert().Model(payrollPerm).Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("create admin payroll permission: %v", err))
		return
	}

	// Update workspace license to admin_create default
	licenseJSON, _ := json.Marshal(h.cfg.AdminCreateDefaultLicense)
	workspace.License = licenseJSON
	if _, err := database.Primary.NewUpdate().Model(&workspace).
		Column("license").WherePK().Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("update workspace license: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"company_identifier": newCompany.Identifier,
	})
}

// UploadCer handles POST /api/Company/upload_cer.
func (h *Company) UploadCer(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	companyObj, _ := auth.CompanyFromContext(r.Context())
	jsonBody := auth.JSONBodyFromContext(r.Context())

	cerB64, _ := jsonBody["cer"].(string)
	keyB64, _ := jsonBody["key"].(string)
	password, _ := jsonBody["pas"].(string)
	if cerB64 == "" || keyB64 == "" || password == "" {
		response.BadRequest(w, "cer, key and pas are required")
		return
	}

	cerBytes, err := base64.StdEncoding.DecodeString(cerB64)
	if err != nil {
		response.BadRequest(w, "invalid base64 for cer")
		return
	}
	keyBytes, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		response.BadRequest(w, "invalid base64 for key")
		return
	}

	cert, err := company.ValidateFIELCertificate(cerBytes, keyBytes, password)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// Assert same RFC
	if companyObj.RFC != nil && *companyObj.RFC != cert.RFC {
		response.BadRequest(w, "The RFC in the certificate is not the same as the one in the company")
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	wsID := int64(0)
	if companyObj.WorkspaceID != nil {
		wsID = *companyObj.WorkspaceID
	}

	// Upload to S3
	if err := h.files.Upload(ctx, h.cfg.S3Certs, s3keys.CertRoute(wsID, companyObj.ID, "cer"), cerBytes); err != nil {
		response.InternalError(w, fmt.Sprintf("upload cer: %v", err))
		return
	}
	if err := h.files.Upload(ctx, h.cfg.S3Certs, s3keys.CertRoute(wsID, companyObj.ID, "key"), keyBytes); err != nil {
		response.InternalError(w, fmt.Sprintf("upload key: %v", err))
		return
	}
	if err := h.files.Upload(ctx, h.cfg.S3Certs, s3keys.CertRoute(wsID, companyObj.ID, "txt"), []byte(password)); err != nil {
		response.InternalError(w, fmt.Sprintf("upload passphrase: %v", err))
		return
	}
	h.mirrorFIELToLocalStack(ctx, wsID, companyObj.ID, cerBytes, keyBytes, password)

	isNew := true
	if (companyObj.HaveCertificates != nil && *companyObj.HaveCertificates) ||
		(companyObj.HasValidCerts != nil && *companyObj.HasValidCerts) {
		isNew = false
	}

	haveCerts := true
	companyObj.HaveCertificates = &haveCerts
	companyObj.HasValidCerts = &haveCerts
	if _, err := database.Primary.NewUpdate().Model(companyObj).
		Column("have_certificates", "has_valid_certs").
		WherePK().Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("update cert flags: %v", err))
		return
	}

	// Publish REQUEST_RESTORE_TRIAL if first cert and no other active company in workspace
	if isNew {
		hasOther, _ := h.workspaceHasOtherActiveCompany(ctx, database, companyObj)
		if !hasOther {
			h.bus.Publish(event.EventTypeRequestRestoreTrial, user)
		}
		// First FIEL on an existing company row (shell / non-create flows): same SAT bootstrap as create.
		rfc := ""
		if companyObj.RFC != nil {
			rfc = *companyObj.RFC
		}
		wsID := int64(0)
		if companyObj.WorkspaceID != nil {
			wsID = *companyObj.WorkspaceID
		}
		h.bus.Publish(event.EventTypeCompanyCreated, event.CompanyCreatedEvent{
			CompanyIdentifier: companyObj.Identifier,
			CompanyRFC:        rfc,
			WorkspaceID:       wsID,
			CompanyID:         companyObj.ID,
		})
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"Successful": "Fiel Uploaded"})
	_ = user
}

// GetCer handles POST /api/Company/get_cer.
func (h *Company) GetCer(w http.ResponseWriter, r *http.Request) {
	companyObj, _ := auth.CompanyFromContext(r.Context())
	ctx := r.Context()

	wsID := int64(0)
	if companyObj.WorkspaceID != nil {
		wsID = *companyObj.WorkspaceID
	}

	cerBytes, err := h.files.Download(ctx, h.cfg.S3Certs, s3keys.CertRoute(wsID, companyObj.ID, "cer"))
	if err != nil {
		response.BadRequest(w, "The company does not have certificates uploaded in the system")
		return
	}

	cert, err := company.ParseCertificateDER(cerBytes)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("parse certificate: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, cert.Info())
}

// Update handles PUT /api/Company/.
func (h *Company) Update(w http.ResponseWriter, r *http.Request) {
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
	if err := crud.ValidateUpdateData(req.Values); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// Validate email list fields
	for _, field := range []string{"emails_to_send_efos", "emails_to_send_errors", "emails_to_send_canceled"} {
		if v, ok := req.Values[field]; ok {
			if _, isList := v.([]interface{}); !isList {
				response.BadRequest(w, fmt.Sprintf("%s must be a list of emails", field))
				return
			}
		}
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	records, err := crud.Update[control.Company](ctx, database.Primary, req.IDs, req.Values)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("update company: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, crud.Serialize(records))
}

// Delete handles DELETE /api/Company/.
func (h *Company) Delete(w http.ResponseWriter, r *http.Request) {
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

	deletedIDs, err := crud.Delete[control.Company](ctx, database.Primary, req.IDs)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("delete company: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"deleted": deletedIDs})
}

// GetData handles GET /api/Company/{cid}/data/{key}.
func (h *Company) GetData(w http.ResponseWriter, r *http.Request) {
	companyObj := h.loadCompanyFromPath(r)
	if companyObj == nil {
		response.NotFound(w, "company not found")
		return
	}
	key := r.PathValue("key")

	var dataMap map[string]interface{}
	if err := json.Unmarshal(companyObj.Data, &dataMap); err != nil {
		response.WriteJSON(w, http.StatusOK, nil)
		return
	}
	response.WriteJSON(w, http.StatusOK, dataMap[key])
}

// SetData handles PUT /api/Company/{cid}/data/{key}.
func (h *Company) SetData(w http.ResponseWriter, r *http.Request) {
	companyObj := h.loadCompanyFromPath(r)
	if companyObj == nil {
		response.NotFound(w, "company not found")
		return
	}
	key := r.PathValue("key")

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

	var dataMap map[string]interface{}
	if err := json.Unmarshal(companyObj.Data, &dataMap); err != nil {
		dataMap = make(map[string]interface{})
	}
	dataMap[key] = req.Value

	newData, _ := json.Marshal(dataMap)
	companyObj.Data = newData

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	if _, err := database.Primary.NewUpdate().Model(companyObj).
		Column("data").WherePK().Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("update data: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"key": key, "value": req.Value})
}

// loadCompanyFromPath loads a company by the {cid} path param or from context.
func (h *Company) loadCompanyFromPath(r *http.Request) *control.Company {
	if c, ok := auth.CompanyFromContext(r.Context()); ok && c != nil {
		return c
	}
	cid := r.PathValue("cid")
	if cid == "" {
		cid = r.PathValue("company_identifier")
	}
	if cid == "" {
		return nil
	}
	var c control.Company
	err := h.database.Primary.NewSelect().Model(&c).
		Where("identifier = ?", cid).Limit(1).Scan(r.Context())
	if err != nil {
		return nil
	}
	return &c
}

// SetISRPercentage handles PUT /api/Company/set_isr_percentage.
func (h *Company) SetISRPercentage(w http.ResponseWriter, r *http.Request) {
	companyObj, _ := auth.CompanyFromContext(r.Context())
	jsonBody := auth.JSONBodyFromContext(r.Context())

	percentage, ok := jsonBody["percentage"].(float64)
	if !ok {
		response.BadRequest(w, "percentage is required and must be a number")
		return
	}

	valid := false
	for _, p := range h.cfg.ISRPercentageList {
		if p == percentage {
			valid = true
			break
		}
	}
	if !valid {
		response.BadRequest(w, fmt.Sprintf("ISR percentage must be one of %v", h.cfg.ISRPercentageList))
		return
	}

	var dataMap map[string]interface{}
	if err := json.Unmarshal(companyObj.Data, &dataMap); err != nil {
		dataMap = make(map[string]interface{})
	}
	dataMap["isr_percentage"] = percentage
	newData, _ := json.Marshal(dataMap)
	companyObj.Data = newData

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	if _, err := database.Primary.NewUpdate().Model(companyObj).
		Column("data").WherePK().Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("update ISR: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"message": "ISR percentage updated"})
}

// GetISRPercentage handles GET /api/Company/get_isr_percentage.
func (h *Company) GetISRPercentage(w http.ResponseWriter, r *http.Request) {
	companyObj, _ := auth.CompanyFromContext(r.Context())

	var dataMap map[string]interface{}
	pct := h.cfg.ISRDefaultPercentage
	if err := json.Unmarshal(companyObj.Data, &dataMap); err == nil {
		if v, ok := dataMap["isr_percentage"].(float64); ok {
			pct = v
		}
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"isr_percentage": pct})
}

// --- internal helpers ---

func (h *Company) createFromCerts(
	ctx context.Context,
	database *db.Database,
	user *control.User,
	workspaceIdentifier string,
	workspaceID int64,
	cerBytes, keyBytes []byte,
	password string,
) (*control.Company, error) {
	cert, err := company.ValidateFIELCertificate(cerBytes, keyBytes, password)
	if err != nil {
		return nil, err
	}

	// Check duplicate RFC in workspace
	var existingCount int
	existingCount, err = database.Primary.NewSelect().
		Model((*control.Company)(nil)).
		Where("workspace_id = ?", workspaceID).
		Where("rfc = ?", cert.RFC).
		Count(ctx)
	if err == nil && existingCount > 0 && cert.RFC != "PGD1009214W0" {
		return nil, fmt.Errorf("RFC %s already exists in this workspace", cert.RFC)
	}

	// Check freemium duplicate limits
	if err := h.ensureNoRFCInFreemiumAccounts(ctx, database, cert.RFC); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	bTrue := true
	bFalse := false
	defaultData, _ := json.Marshal(map[string]interface{}{})
	identifier := crud.NewIdentifier()

	newCompany := &control.Company{
		Identifier:          identifier,
		Name:                cert.Name,
		RFC:                 &cert.RFC,
		WorkspaceID:         &workspaceID,
		WorkspaceIdentifier: &workspaceIdentifier,
		Active:              &bTrue,
		HaveCertificates:    &bFalse,
		HasValidCerts:       &bFalse,
		HistoricDownloaded:  &bFalse,
		ExceedMetadataLimit: &bFalse,
		PermissionToSync:    &bFalse,
		ADDAutoSync:         &bFalse,
		Data:                defaultData,
		TenantDBName:        &h.cfg.DBName,
		TenantDBHost:        &h.cfg.DBHost,
		TenantDBPort:        &h.cfg.DBPort,
		TenantDBUser:        &h.cfg.DBUser,
		TenantDBPassword:    &h.cfg.DBPassword,
		TenantDBSchema:      &identifier,
		CreatedAt:           now,
	}

	// Tenant DDL before control insert so we never persist a company whose schema is empty.
	// On cloud, missing Python/Alembic (e.g. scratch image) must fail loudly — see Dockerfile.azure.
	if err := h.runTenantMigrations(ctx, database, identifier); err != nil {
		if h.cfg.LocalInfra {
			slog.Warn("tenant migrations failed (non-fatal in local dev)", "schema", identifier, "error", err)
		} else {
			return nil, fmt.Errorf("tenant migrations: %w", err)
		}
	}

	if _, err := database.Primary.NewInsert().Model(newCompany).Exec(ctx); err != nil {
		return nil, fmt.Errorf("insert company: %w", err)
	}

	// Upload certs to blob/S3 (keys: ws_<workspaceID>/c_<companyID>.{cer,key,txt})
	if err := h.files.Upload(ctx, h.cfg.S3Certs, s3keys.CertRoute(workspaceID, newCompany.ID, "cer"), cerBytes); err != nil {
		return nil, fmt.Errorf("upload cer: %w", err)
	}
	if err := h.files.Upload(ctx, h.cfg.S3Certs, s3keys.CertRoute(workspaceID, newCompany.ID, "key"), keyBytes); err != nil {
		return nil, fmt.Errorf("upload key: %w", err)
	}
	if err := h.files.Upload(ctx, h.cfg.S3Certs, s3keys.CertRoute(workspaceID, newCompany.ID, "txt"), []byte(password)); err != nil {
		return nil, fmt.Errorf("upload passphrase: %w", err)
	}
	h.mirrorFIELToLocalStack(ctx, workspaceID, newCompany.ID, cerBytes, keyBytes, password)

	newCompany.HaveCertificates = &bTrue
	newCompany.HasValidCerts = &bTrue
	if _, err := database.Primary.NewUpdate().Model(newCompany).
		Column("have_certificates", "has_valid_certs").
		WherePK().Exec(ctx); err != nil {
		return nil, fmt.Errorf("update cert flags: %w", err)
	}

	// Create initial permissions for the user
	for _, role := range []string{control.RoleOperator, control.RolePayroll} {
		perm := &control.Permission{
			Identifier: crud.NewIdentifier(),
			UserID:     user.ID,
			CompanyID:  newCompany.ID,
			Role:       role,
		}
		if _, err := database.Primary.NewInsert().Model(perm).Exec(ctx); err != nil {
			return nil, fmt.Errorf("create permission %s: %w", role, err)
		}
	}

	// Publish COMPANY_CREATED event
	rfc := ""
	if newCompany.RFC != nil {
		rfc = *newCompany.RFC
	}
	h.bus.Publish(event.EventTypeCompanyCreated, event.CompanyCreatedEvent{
		CompanyIdentifier: newCompany.Identifier,
		CompanyRFC:        rfc,
		WorkspaceID:       workspaceID,
		CompanyID:         newCompany.ID,
	})

	return newCompany, nil
}

func (h *Company) runTenantMigrations(ctx context.Context, database *db.Database, schema string) error {
	slog.Warn("tenant_migration_start", "schema", schema, "cloud", h.cfg.CloudProvider)
	if err := db.ValidateCompanyTenantSchema(schema); err != nil {
		return err
	}
	_, err := database.Primary.ExecContext(ctx,
		fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, schema))
	if err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if err := db.ApplyEmbeddedTenantDDL(ctx, database.Primary, schema); err != nil {
		slog.Warn("tenant_migration_failed", "schema", schema, "error", err.Error())
		return fmt.Errorf("tenant ddl: %w", err)
	}
	slog.Info("tenant migrations completed", "schema", schema)
	return nil
}

func (h *Company) ensureNoRFCInFreemiumAccounts(ctx context.Context, database *db.Database, rfc string) error {
	if h.cfg.SpecialRFCs[rfc] {
		return nil
	}

	query := `
		SELECT COUNT(*)
		FROM company
		INNER JOIN workspace ON company.workspace_identifier = workspace.identifier
		WHERE company.rfc = ?
		AND (workspace.license::jsonb -> 'details' -> 'products' @> ?
		OR workspace.license::jsonb ->> 'stripe_status' <> 'active')
	`
	productsJSON, _ := json.Marshal([]map[string]interface{}{{"identifier": h.cfg.ProductTrial}})
	var count int
	err := database.Primary.QueryRowContext(ctx, query, rfc, string(productsJSON)).Scan(&count)
	if err != nil {
		return nil // non-fatal
	}
	if count >= h.cfg.MaxSameCompanyInTrials {
		return fmt.Errorf("Company RFC already exists in another freemium workspace")
	}
	return nil
}

func (h *Company) workspaceHasOtherActiveCompany(ctx context.Context, database *db.Database, c *control.Company) (bool, error) {
	if c.WorkspaceID == nil {
		return false, nil
	}
	count, err := database.Primary.NewSelect().
		Model((*control.Company)(nil)).
		Where("workspace_id = ?", *c.WorkspaceID).
		Where("id != ?", c.ID).
		Where("active = true").
		Where("have_certificates = true").
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *Company) getOrCreateUserByEmail(ctx context.Context, database *db.Database, email string) (*control.User, error) {
	var user control.User
	err := database.Primary.NewSelect().Model(&user).
		Where("email = ?", email).
		Limit(1).Scan(ctx)
	if err == nil {
		// Check if user has a workspace
		var wsCount int
		wsCount, _ = database.Primary.NewSelect().
			Model((*control.Workspace)(nil)).
			Where("owner_id = ?", user.ID).
			Count(ctx)
		if wsCount == 0 {
			h.createDefaultWorkspace(ctx, database, &user)
		}
		return &user, nil
	}

	// Create new user
	sub := crud.NewIdentifier()
	phone := "3313603245"
	user = control.User{
		Identifier: crud.NewIdentifier(),
		Name:       &email,
		Email:      email,
		CognitoSub: &sub,
		Phone:      &phone,
	}
	if _, err := database.Primary.NewInsert().Model(&user).Exec(ctx); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	h.createDefaultWorkspace(ctx, database, &user)
	return &user, nil
}

func (h *Company) createDefaultWorkspace(ctx context.Context, database *db.Database, user *control.User) {
	name := ""
	if user.Name != nil {
		name = *user.Name + "'s Workspace"
	}
	now := time.Now().UTC()
	validUntil := now.Add(h.cfg.DefaultLicenseLifetime)
	stripeStatus := "trial"
	defaultLicense, _ := json.Marshal(map[string]interface{}{
		"date_start": now.Format(crud.APITimestampFormat),
		"date_end":   now.Add(h.cfg.DefaultLicenseLifetime).Format(crud.APITimestampFormat),
		"details": map[string]interface{}{
			"max_companies":     1,
			"max_emails_enroll": 1,
		},
	})

	ws := &control.Workspace{
		Identifier:   crud.NewIdentifier(),
		Name:         &name,
		OwnerID:      &user.ID,
		License:      defaultLicense,
		ValidUntil:   &validUntil,
		StripeStatus: &stripeStatus,
	}
	database.Primary.NewInsert().Model(ws).Exec(ctx)
}

func populateCompanyEmails(c *control.Company, email string) {
	emailList, _ := json.Marshal([]string{email})
	c.EmailsToSendEfos = emailList
	c.EmailsToSendErrors = emailList
	c.EmailsToSendCanceled = emailList
}

// mirrorFIELToLocalStack duplicates cer/key/passphrase to LocalStack S3 when certMirror is set
// (hybrid local: Azurite primary, Python worker reads S3_CERTS from LocalStack).
func (h *Company) mirrorFIELToLocalStack(ctx context.Context, workspaceID, companyID int64, cerBytes, keyBytes []byte, password string) {
	if h.certMirror == nil {
		return
	}
	bucket := h.cfg.S3Certs
	type pair struct {
		ext  string
		data []byte
	}
	for _, p := range []pair{
		{"cer", cerBytes},
		{"key", keyBytes},
		{"txt", []byte(password)},
	} {
		key := s3keys.CertRoute(workspaceID, companyID, p.ext)
		if err := h.certMirror.Upload(ctx, bucket, key, p.data); err != nil {
			slog.Warn("cert_mirror: upload failed", "key", key, "error", err)
		}
	}
}

func strPtr(s string) *string { return &s }
