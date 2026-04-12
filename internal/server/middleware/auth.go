package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Auth struct {
	decoder  *auth.JWTDecoder
	database *db.Database
	cfg      *config.Config
}

func NewAuth(cfg *config.Config, database *db.Database, decoder *auth.JWTDecoder) *Auth {
	return &Auth{
		decoder:  decoder,
		database: database,
		cfg:      cfg,
	}
}

// RequireAuth validates the access_token header, looks up the User by cognito_sub,
// and attaches the User to the request context.
func (a *Auth) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("access_token")
		if token == "" {
			response.Unauthorized(w, "Unauthorized")
			return
		}

		claims, err := a.decoder.Decode(token)
		if err != nil {
			response.Unauthorized(w, "Unauthorized")
			return
		}

		var user control.User
		err = a.database.Primary.NewSelect().
			Model(&user).
			Where("cognito_sub = ?", claims.Sub).
			Limit(1).
			Scan(r.Context())
		if err != nil {
			response.Unauthorized(w, "Unauthorized")
			return
		}

		ctx := auth.WithUser(r.Context(), &user)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// RequireCompany requires auth + extracts company_identifier from the request,
// verifies user has OPERATOR permission, and attaches company to context.
func (a *Auth) RequireCompany(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())

		body := make(map[string]interface{})
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			raw, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err == nil && len(raw) > 0 {
				_ = json.Unmarshal(raw, &body)
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}

		cid := extractCompanyIdentifier(r, body)
		if cid == "" {
			response.Unauthorized(w, "No company identifier provided")
			return
		}

		var company control.Company
		err := a.database.Primary.NewSelect().
			Model(&company).
			Where("identifier = ?", cid).
			Limit(1).
			Scan(r.Context())
		if err != nil {
			response.Unauthorized(w, "No company found with the given identifier")
			return
		}

		count, err := a.database.Primary.NewSelect().
			Model((*control.Permission)(nil)).
			Where("user_id = ?", user.ID).
			Where("company_id = ?", company.ID).
			Where("role = ?", control.RoleOperator).
			Count(r.Context())
		if err != nil || count == 0 {
			response.Unauthorized(w, "No company found")
			return
		}

		ctx := auth.WithCompanyIdentifier(r.Context(), cid)
		ctx = auth.WithCompany(ctx, &company)
		ctx = auth.WithJSONBody(ctx, body)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin requires auth + verifies user email is in ADMIN_EMAILS list.
func (a *Auth) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())
		if !auth.IsAdmin(user.Email, a.cfg.AdminEmails) {
			response.Forbidden(w, "Only admin users can perform this action")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdminOrCompanyOperator allows ADMIN_EMAILS users, or any authenticated user
// with OPERATOR permission on the company identified in the JSON body or
// company_identifier header (same rules as RequireCompany).
func (a *Auth) RequireAdminOrCompanyOperator(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())

		body := make(map[string]interface{})
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			raw, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err == nil && len(raw) > 0 {
				_ = json.Unmarshal(raw, &body)
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}

		if auth.IsAdmin(user.Email, a.cfg.AdminEmails) {
			ctx := auth.WithJSONBody(r.Context(), body)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cid := extractCompanyIdentifier(r, body)
		if cid == "" {
			response.Forbidden(w, "company_identifier is required for non-admin users")
			return
		}

		var company control.Company
		err := a.database.Primary.NewSelect().
			Model(&company).
			Where("identifier = ?", cid).
			Limit(1).
			Scan(r.Context())
		if err != nil {
			response.Forbidden(w, "No company found with the given identifier")
			return
		}

		count, err := a.database.Primary.NewSelect().
			Model((*control.Permission)(nil)).
			Where("user_id = ?", user.ID).
			Where("company_id = ?", company.ID).
			Where("role = ?", control.RoleOperator).
			Count(r.Context())
		if err != nil || count == 0 {
			response.Forbidden(w, "Only admin users or company operators can perform this action")
			return
		}

		ctx := auth.WithCompanyIdentifier(r.Context(), cid)
		ctx = auth.WithCompany(ctx, &company)
		ctx = auth.WithJSONBody(ctx, body)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdminCreate is like RequireAdmin but returns 401 instead of 403
// (matches Python's get_admin_create_user dependency).
func (a *Auth) RequireAdminCreate(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())
		if !auth.IsAdmin(user.Email, a.cfg.AdminEmails) {
			response.Unauthorized(w,
				"User does not have permission to create a company. "+
					"Please contact support if you believe this is an error.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractCompanyIdentifier tries body → domain → header → path params,
// matching Python's check_body / check_domain / check_header / check_uri_params.
func extractCompanyIdentifier(r *http.Request, body map[string]interface{}) string {
	if cid, ok := body["company_identifier"].(string); ok && cid != "" {
		return cid
	}

	if domain, ok := body["domain"].([]interface{}); ok {
		for i, item := range domain {
			tuple, ok := item.([]interface{})
			if !ok || len(tuple) < 3 {
				continue
			}
			field, _ := tuple[0].(string)
			op, _ := tuple[1].(string)
			if field == "company_identifier" && op == "=" {
				if val, ok := tuple[2].(string); ok {
					body["domain"] = append(domain[:i], domain[i+1:]...)
					return val
				}
			}
		}
	}

	if cid := r.Header.Get("company_identifier"); cid != "" {
		return cid
	}

	if cid := r.PathValue("company_identifier"); cid != "" {
		return cid
	}
	if cid := r.PathValue("cid"); cid != "" {
		return cid
	}

	return ""
}
