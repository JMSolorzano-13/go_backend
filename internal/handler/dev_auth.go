package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type DevAuth struct {
	cfg      *config.Config
	database *db.Database
}

func NewDevAuth(cfg *config.Config, database *db.Database) *DevAuth {
	return &DevAuth{cfg: cfg, database: database}
}

func (h *DevAuth) ensureLocalMode(w http.ResponseWriter) bool {
	if !h.cfg.LocalInfra {
		response.Forbidden(w,
			"Development auth endpoints are only available when LOCAL_INFRA=1. "+
				"These endpoints are disabled in production for security.")
		return false
	}
	return true
}

func cognitoSubFromEmail(email string) string {
	sub := strings.ReplaceAll(email, "@", "-")
	sub = strings.ReplaceAll(sub, ".", "-")
	return "local-" + sub
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// Login creates or finds a user by email and returns mock JWT tokens.
// POST /api/dev/login  {"email": "..."}
func (h *DevAuth) Login(w http.ResponseWriter, r *http.Request) {
	if !h.ensureLocalMode(w) {
		return
	}

	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		response.Forbidden(w, "Email is required")
		return
	}

	ctx := r.Context()
	cognitoSub := cognitoSubFromEmail(body.Email)

	var user control.User
	err := h.database.Primary.NewSelect().
		Model(&user).
		Where("email = ?", body.Email).
		Limit(1).
		Scan(ctx)

	if err != nil {
		phone := "3313603245"
		user = control.User{
			Identifier: newUUID(),
			Email:      body.Email,
			Name:       &body.Email,
			CognitoSub: &cognitoSub,
			Phone:      &phone,
		}
		if _, err := h.database.Primary.NewInsert().Model(&user).Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("create user: %v", err))
			return
		}
	} else {
		user.CognitoSub = &cognitoSub
		if _, err := h.database.Primary.NewUpdate().
			Model(&user).
			Column("cognito_sub").
			WherePK().
			Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("update user: %v", err))
			return
		}
	}

	idToken, err := auth.CreateMockToken(body.Email, cognitoSub)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("create token: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"idToken":      idToken,
		"accessToken":  idToken,
		"refreshToken": fmt.Sprintf("mock-refresh-%s", cognitoSub),
		"expiresIn":    86400,
		"tokenType":    "Bearer",
		"user": map[string]interface{}{
			"id":    user.ID,
			"name":  user.Name,
			"email": user.Email,
		},
	})
}

// GenerateToken returns fresh mock tokens for an existing user.
// POST /api/dev/token  {"email": "..."}
func (h *DevAuth) GenerateToken(w http.ResponseWriter, r *http.Request) {
	if !h.ensureLocalMode(w) {
		return
	}

	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		response.Forbidden(w, "Email is required")
		return
	}

	var user control.User
	err := h.database.Primary.NewSelect().
		Model(&user).
		Where("email = ?", body.Email).
		Limit(1).
		Scan(r.Context())
	if err != nil {
		response.Forbidden(w,
			fmt.Sprintf("User with email %s not found. Use /dev/login to create a new user.", body.Email))
		return
	}

	cognitoSub := cognitoSubFromEmail(body.Email)
	idToken, err := auth.CreateMockToken(body.Email, cognitoSub)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("create token: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"idToken":      idToken,
		"accessToken":  idToken,
		"refreshToken": fmt.Sprintf("mock-refresh-%s", cognitoSub),
		"expiresIn":    86400,
		"tokenType":    "Bearer",
	})
}

// ListUsers returns up to 100 users from the control DB.
// GET /api/dev/users
func (h *DevAuth) ListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.ensureLocalMode(w) {
		return
	}

	var users []control.User
	err := h.database.Primary.NewSelect().
		Model(&users).
		Limit(100).
		Scan(r.Context())
	if err != nil {
		response.InternalError(w, fmt.Sprintf("query users: %v", err))
		return
	}

	userList := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		userList = append(userList, map[string]interface{}{
			"id":          u.ID,
			"email":       u.Email,
			"name":        u.Name,
			"cognito_sub": u.CognitoSub,
			"identifier":  u.Identifier,
		})
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"users": userList,
		"total": len(userList),
	})
}

// AuthStatus reports the current auth configuration.
// GET /api/dev/auth-status
func (h *DevAuth) AuthStatus(w http.ResponseWriter, r *http.Request) {
	if !h.ensureLocalMode(w) {
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"local_mode":           true,
		"cognito_mocked":       true,
		"local_infra":          h.cfg.LocalInfra,
		"cognito_client_id":    h.cfg.CognitoClientID,
		"cognito_user_pool_id": h.cfg.CognitoUserPoolID,
		"message": "Running in local development mode with mocked authentication. " +
			"JWT signatures are not verified. Use /dev/login to get test tokens.",
	})
}
