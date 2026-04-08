package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

type User struct {
	cfg      *config.Config
	database *db.Database
	idp      port.IdentityProvider
	jwt      *auth.JWTDecoder
}

func NewUser(cfg *config.Config, database *db.Database, idp port.IdentityProvider, jwtDecoder *auth.JWTDecoder) *User {
	return &User{cfg: cfg, database: database, idp: idp, jwt: jwtDecoder}
}

// Auth handles POST /api/User/auth — Cognito auth flow.
func (h *User) Auth(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Flow   string            `json:"flow"`
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if h.cfg.BlockAppAccess {
		response.WriteJSON(w, http.StatusForbidden, map[string]string{
			"state": h.cfg.BlockAppMessage,
		})
		return
	}

	out, err := h.idp.InitiateAuth(r.Context(), req.Flow, req.Params)
	if err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	if out.ChallengeName != "" {
		response.WriteJSON(w, 428, map[string]string{
			"state":             "need_cognito_challenge",
			"challenge_name":    out.ChallengeName,
			"challenge_session": out.ChallengeSession,
		})
		return
	}

	if out.Tokens == nil {
		response.Forbidden(w, "no authentication result")
		return
	}
	response.WriteJSON(w, http.StatusOK, out.Tokens)
}

// AuthByCode handles GET /api/User/auth/{code} — OAuth2 code exchange.
func (h *User) AuthByCode(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		response.BadRequest(w, "code is required")
		return
	}

	if h.cfg.BlockAppAccess {
		response.WriteJSON(w, http.StatusForbidden, map[string]string{
			"state": h.cfg.BlockAppMessage,
		})
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	tokens, err := h.exchangeCodeForTokens(code)
	if err != nil {
		response.Unauthorized(w, err.Error())
		return
	}

	// Link user to DB if needed
	idToken, _ := tokens["id_token"].(string)
	if idToken != "" {
		h.linkToDB(ctx, database, idToken)
	}

	response.WriteJSON(w, http.StatusOK, tokens)
}

// AuthChallenge handles POST /api/User/auth_challenge.
func (h *User) AuthChallenge(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		ChallengeName    string `json:"challenge_name"`
		ChallengeSession string `json:"challenge_session"`
		Email            string `json:"email"`
		Password         string `json:"password"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if req.ChallengeName != "NEW_PASSWORD_REQUIRED" {
		response.Unauthorized(w, fmt.Sprintf("Challenge '%s' not implemented", req.ChallengeName))
		return
	}

	out, err := h.idp.RespondToAuthChallenge(r.Context(), req.ChallengeName, req.ChallengeSession, req.Email, req.Password)
	if err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	if out.Tokens == nil {
		response.Forbidden(w, "no authentication result")
		return
	}
	response.WriteJSON(w, http.StatusOK, out.Tokens)
}

// CreateUser handles POST /api/User/ — signup flow.
func (h *User) CreateUser(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Name       string `json:"name"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		SourceName string `json:"source_name"`
		Phone      string `json:"phone"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	// Check if user exists
	var existing control.User
	err = database.Primary.NewSelect().Model(&existing).
		Where("lower(trim(email)) = ?", req.Email).Limit(1).Scan(ctx)
	if err == nil {
		response.Forbidden(w, "User already exists")
		return
	}

	var cognitoSub string
	var passwordHash *string

	if h.cfg.LocalInfra {
		cognitoSub = "local-signup-" + strings.ReplaceAll(req.Email, "@", "-")
	} else if h.cfg.CloudProvider == "azure" {
		cognitoSub = uuid.New().String()
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			response.InternalError(w, "password hash failed")
			return
		}
		hashStr := string(hash)
		passwordHash = &hashStr
	} else {
		cognitoSub, err = h.idp.SignUp(ctx, req.Email, req.Password)
		if err != nil {
			response.Forbidden(w, err.Error())
			return
		}
	}

	user := &control.User{
		Identifier:   crud.NewIdentifier(),
		Name:         strPtr(req.Name),
		Email:        req.Email,
		PasswordHash: passwordHash,
		CognitoSub:   &cognitoSub,
		SourceName:   nilIfEmpty(req.SourceName),
		Phone:        nilIfEmpty(req.Phone),
	}

	if _, err := database.Primary.NewInsert().Model(user).Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("create user: %v", err))
		return
	}

	// Create default workspace
	h.createDefaultWorkspaceForUser(ctx, database, user)

	response.WriteJSON(w, http.StatusOK, crud.SerializeOne(*user))
}

// GetUser handles GET /api/User/ — returns user info with access map.
func (h *User) GetUser(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	basicInfo := map[string]interface{}{
		"id":    user.ID,
		"name":  user.Name,
		"email": user.Email,
	}

	access, err := h.getUserAccess(ctx, database, user)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("get access: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"user":   basicInfo,
		"access": access,
	})
}

// UpdateUser handles PUT /api/User/.
func (h *User) UpdateUser(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Values map[string]interface{} `json:"values"`
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

	if err := crud.ValidateUpdateData(req.Values); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	records, err := crud.Update[control.User](ctx, database.Primary, []string{user.Identifier}, req.Values)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("update user: %v", err))
		return
	}
	if len(records) > 0 {
		response.WriteJSON(w, http.StatusOK, crud.SerializeOne(records[0]))
	} else {
		response.WriteJSON(w, http.StatusOK, crud.SerializeOne(*user))
	}
}

// ChangePassword handles POST /api/User/change_password.
func (h *User) ChangePassword(w http.ResponseWriter, r *http.Request) {
	accessToken := r.Header.Get("access_token")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Email           string `json:"email"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	err = h.idp.ChangePassword(r.Context(), accessToken, req.CurrentPassword, req.NewPassword)
	if err != nil {
		response.Forbidden(w, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{})
}

// Forgot handles POST /api/User/forgot.
func (h *User) Forgot(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	out, err := h.idp.ForgotPassword(r.Context(), req.Email)
	if err != nil {
		response.Forbidden(w, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, out)
}

// ConfirmForgot handles POST /api/User/confirm_forgot.
func (h *User) ConfirmForgot(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Email            string `json:"email"`
		VerificationCode string `json:"verification_code"`
		NewPassword      string `json:"new_password"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	err = h.idp.ConfirmForgotPassword(r.Context(), req.Email, req.VerificationCode, req.NewPassword)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{})
}

// PostConfig handles POST /api/User/config — set user config per company (tenant session).
func (h *User) PostConfig(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	cid, _ := auth.CompanyIdentifierFromContext(r.Context())
	jsonBody := auth.JSONBodyFromContext(r.Context())

	configData, ok := jsonBody["config"]
	if !ok {
		response.BadRequest(w, "config is required")
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, cid, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	configJSON, _ := json.Marshal(configData)

	// Upsert user config
	uc := &tenant.UserConfig{
		UserIdentifier: user.Identifier,
		Data:           configJSON,
	}
	_, err = conn.NewInsert().Model(uc).
		On("CONFLICT (user_identifier) DO UPDATE").
		Set("data = EXCLUDED.data").
		Exec(ctx)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("set config: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, configData)
}

// GetConfig handles GET /api/User/config/{company_identifier}.
func (h *User) GetConfig(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	cid := r.PathValue("company_identifier")

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, cid, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	defaultDashboardIds := []string{"totals", "linecharttotals", "nominal-income", "improved-IVA"}
	// Defaults match Python chalicelib/schema/models/tenant/user_config.py DEFAULT_DATA (+ frontend Extra.DEFAULT_USER_CONFIG).
	defaultValidationIDs := []string{"issuedcfdis", "receivedcfdis", "efos"}
	defaultIVAIDs := []string{"iva-widget"}

	mergeUserConfigDefaults := func(data map[string]interface{}) {
		if _, ok := data["dashboardIds"]; !ok {
			data["dashboardIds"] = defaultDashboardIds
		}
		if _, ok := data["validationIds"]; !ok {
			data["validationIds"] = defaultValidationIDs
		}
		if _, ok := data["IVAIds"]; !ok {
			data["IVAIds"] = defaultIVAIDs
		}
		if _, ok := data["pivotLayouts"]; !ok {
			data["pivotLayouts"] = map[string]interface{}{}
		}
		if _, ok := data["tableColumns"]; !ok {
			data["tableColumns"] = map[string]interface{}{}
		}
	}

	var uc tenant.UserConfig
	err = conn.NewSelect().Model(&uc).
		Where("user_identifier = ?", user.Identifier).
		Scan(ctx)
	if err != nil {
		// No config row — return defaults so the frontend dashboard always renders
		body := map[string]interface{}{
			"dashboardIds":  defaultDashboardIds,
			"validationIds": defaultValidationIDs,
			"IVAIds":        defaultIVAIDs,
			"pivotLayouts":  map[string]interface{}{},
			"tableColumns":  map[string]interface{}{},
		}
		response.WriteJSON(w, http.StatusOK, body)
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal(uc.Data, &data); err != nil || data == nil {
		data = map[string]interface{}{}
	}
	mergeUserConfigDefaults(data)
	response.WriteJSON(w, http.StatusOK, data)
}

// SuperInvite handles POST /api/User/super_invite.
func (h *User) SuperInvite(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req struct {
		Email string `json:"email"`
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

	// Find or create user by email
	var targetUser control.User
	err = database.Primary.NewSelect().Model(&targetUser).
		Where("email = ?", req.Email).Limit(1).Scan(ctx)
	if err != nil {
		// User doesn't exist — invite via Cognito or local mock
		var cognitoSub string
		if h.cfg.LocalInfra {
			cognitoSub = "local-invite-" + strings.ReplaceAll(req.Email, "@", "-")
		} else {
			var createErr error
			cognitoSub, createErr = h.idp.AdminCreateUser(ctx, req.Email, randomPassword())
			if createErr != nil {
				response.Forbidden(w, createErr.Error())
				return
			}
		}
		targetUser = control.User{
			Identifier:  crud.NewIdentifier(),
			Name:        strPtr(req.Email),
			Email:       req.Email,
			CognitoSub:  &cognitoSub,
			InvitedByID: &user.ID,
		}
		if _, err := database.Primary.NewInsert().Model(&targetUser).Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("create invited user: %v", err))
			return
		}
	}

	response.WriteJSON(w, http.StatusOK, crud.SerializeOne(targetUser))
}

// SetEmail handles PUT /api/User/set_email/{old_email}/{new_email} — admin only.
func (h *User) SetEmail(w http.ResponseWriter, r *http.Request) {
	oldEmail := strings.TrimSpace(strings.ToLower(r.PathValue("old_email")))
	newEmail := strings.TrimSpace(strings.ToLower(r.PathValue("new_email")))

	if !emailRegex.MatchString(newEmail) {
		response.BadRequest(w, "Invalid email format")
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	// Check new email not in use
	var existing control.User
	err := database.Primary.NewSelect().Model(&existing).
		Where("email = ?", newEmail).Limit(1).Scan(ctx)
	if err == nil {
		response.BadRequest(w, "Email is already in use")
		return
	}

	// Find target user
	var target control.User
	err = database.Primary.NewSelect().Model(&target).
		Where("email = ?", oldEmail).Limit(1).Scan(ctx)
	if err != nil {
		response.NotFound(w, fmt.Sprintf("User with email '%s' not found", oldEmail))
		return
	}

	target.Email = newEmail
	if _, err := database.Primary.NewUpdate().Model(&target).
		Column("email").WherePK().Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("update email: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UpdateFiscalData handles POST /api/User/update_fiscal_data.
// Phase 11 will implement full Odoo integration; for now returns a stub.
func (h *User) UpdateFiscalData(w http.ResponseWriter, r *http.Request) {
	if h.cfg.MockOdoo {
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "mocked"})
		return
	}
	response.BadRequest(w, "Odoo integration not yet available in Go backend (Phase 11)")
}

// GetFiscalData handles GET /api/User/update_fiscal_data.
func (h *User) GetFiscalData(w http.ResponseWriter, r *http.Request) {
	if h.cfg.MockOdoo {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	response.BadRequest(w, "Odoo integration not yet available in Go backend (Phase 11)")
}

// --- internal helpers ---

var modulesByRole = map[string][]string{
	control.RoleOperator: {"SATSync"},
	control.RolePayroll:  {"SATSync", "Payroll"},
}

func getModulesForRoles(roles []string) []string {
	seen := make(map[string]bool)
	var modules []string
	for _, role := range roles {
		for _, m := range modulesByRole[role] {
			if !seen[m] {
				seen[m] = true
				modules = append(modules, m)
			}
		}
	}
	if modules == nil {
		modules = []string{}
	}
	return modules
}

func (h *User) getUserAccess(ctx context.Context, database *db.Database, user *control.User) (map[string]interface{}, error) {
	var permissions []control.Permission
	err := database.Replica.NewSelect().Model(&permissions).
		Relation("Company").
		Where("p.user_id = ?", user.ID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	workspaceIDs := make(map[int64]bool)
	companyByWS := make(map[int64][]control.Permission)
	for _, p := range permissions {
		if p.Company != nil && p.Company.WorkspaceID != nil {
			wsID := *p.Company.WorkspaceID
			workspaceIDs[wsID] = true
			companyByWS[wsID] = append(companyByWS[wsID], p)
		}
	}

	var ownedWS []control.Workspace
	database.Replica.NewSelect().Model(&ownedWS).
		Where("owner_id = ?", user.ID).Scan(ctx)
	for _, ws := range ownedWS {
		workspaceIDs[ws.ID] = true
	}

	var wsIDs []int64
	for id := range workspaceIDs {
		wsIDs = append(wsIDs, id)
	}
	var workspaces []control.Workspace
	if len(wsIDs) > 0 {
		database.Replica.NewSelect().Model(&workspaces).
			Where("id IN (?)", bun.In(wsIDs)).Scan(ctx)
	}

	access := make(map[string]interface{})
	for _, ws := range workspaces {
		companies := make(map[string]interface{})

		// Collect roles per company from permissions
		companyRoles := make(map[string][]string)
		companyData := make(map[string]*control.Company)
		for _, p := range companyByWS[ws.ID] {
			if p.Company != nil {
				cid := p.Company.Identifier
				companyRoles[cid] = append(companyRoles[cid], p.Role)
				companyData[cid] = p.Company
			}
		}

		for cid, c := range companyData {
			companies[cid] = map[string]interface{}{
				"id":      c.ID,
				"name":    c.Name,
				"modules": getModulesForRoles(companyRoles[cid]),
			}
		}

		var licenseData interface{}
		json.Unmarshal(ws.License, &licenseData)

		access[ws.Identifier] = map[string]interface{}{
			"id":                ws.ID,
			"license":           licenseData,
			"name":              ws.Name,
			"stripe_status":     ws.StripeStatus,
			"owner_id":          ws.OwnerID,
			"pasto_worker_id":   ws.PastoWorkerID,
			"pasto_license_key": ws.PastoLicenseKey,
			"pasto_installed":   ws.PastoInstalled,
			"companies":         companies,
		}
	}

	return access, nil
}

func (h *User) linkToDB(ctx context.Context, database *db.Database, idToken string) {
	claims, err := h.jwt.Decode(idToken)
	if err != nil {
		return
	}

	// Check if user with this sub already exists
	var existing control.User
	err = database.Primary.NewSelect().Model(&existing).
		Where("cognito_sub = ?", claims.Sub).Limit(1).Scan(ctx)
	if err == nil {
		return // already linked
	}

	// Find by email (name claim in Cognito B2C tokens)
	email := claims.Name
	if email == "" {
		email = claims.Email
	}
	var user control.User
	err = database.Primary.NewSelect().Model(&user).
		Where("email = ?", email).Limit(1).Scan(ctx)
	if err != nil {
		return
	}

	user.CognitoSub = &claims.Sub
	database.Primary.NewUpdate().Model(&user).Column("cognito_sub").WherePK().Exec(ctx)
}

func (h *User) exchangeCodeForTokens(code string) (map[string]interface{}, error) {
	if h.cfg.CognitoURL == "" || h.cfg.CognitoURL == "https://placeholder.auth.example.com" {
		return nil, fmt.Errorf("OAuth2 code exchange not configured (no COGNITO_URL / OIDC_TOKEN_URL)")
	}
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {h.cfg.CognitoClientID},
		"code":         {code},
		"redirect_uri": {h.cfg.CognitoRedirectURI},
	}
	if h.cfg.CognitoClientSecret != "" {
		data.Set("client_secret", h.cfg.CognitoClientSecret)
	}

	tokenURL := h.cfg.CognitoURL + "/oauth2/token"

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		result["probable_cause"] = "Maybe the code has already been used or is invalid."
		return nil, fmt.Errorf("%v", result)
	}

	return result, nil
}

func (h *User) createDefaultWorkspaceForUser(ctx context.Context, database *db.Database, user *control.User) {
	companyH := &Company{cfg: h.cfg, database: database}
	companyH.createDefaultWorkspace(ctx, database, user)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func randomPassword() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*"
	b := make([]byte, 12)
	for i := range b {
		b[i] = chars[i%len(chars)]
	}
	// Ensure at least one of each type
	b[0] = 'A'
	b[1] = 'a'
	b[2] = '1'
	b[3] = '!'
	return string(b)
}
