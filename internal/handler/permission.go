package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Permission struct {
	cfg      *config.Config
	database *db.Database
}

func NewPermission(cfg *config.Config, database *db.Database) *Permission {
	return &Permission{cfg: cfg, database: database}
}

var permissionMeta = crud.ModelMeta{
	DefaultOrderBy: "id ASC",
	// Frontend sends e.g. ["user.email", "not in", [...]] (see api/user.ts fetchPermissions).
	Relations: []string{"User", "Company"},
}

// Search handles POST /api/Permission/search — no auth required.
func (h *Permission) Search(w http.ResponseWriter, r *http.Request) {
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
	result, err := crud.Search[control.Permission](r.Context(), h.database.Replica, params, permissionMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, result)
}

// SetPermissions handles PUT /api/Permission/ — bulk permission assignment.
//
// Python source: UserController.set_permissions(emails, permissions_by_company, context, session).
// Resolves users by email, deletes existing permissions, and creates new ones.
// User creation and license validation are deferred to Phase 7 (user management).
func (h *Permission) SetPermissions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var req struct {
		Emails               []string            `json:"emails"`
		PermissionsByCompany map[string][]string `json:"permissions"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.Emails) == 0 {
		response.BadRequest(w, "emails is required")
		return
	}
	if len(req.PermissionsByCompany) == 0 {
		response.BadRequest(w, "permissions is required")
		return
	}

	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	tx, err := database.Primary.BeginTx(ctx, nil)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("begin tx: %v", err))
		return
	}
	defer tx.Rollback()

	var users []control.User
	if err := tx.NewSelect().Model(&users).
		Where("email IN (?)", bun.In(req.Emails)).
		Scan(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("query users: %v", err))
		return
	}

	companyIdentifiers := make([]string, 0, len(req.PermissionsByCompany))
	for id := range req.PermissionsByCompany {
		companyIdentifiers = append(companyIdentifiers, id)
	}

	var companies []control.Company
	if err := tx.NewSelect().Model(&companies).
		Where("identifier IN (?)", bun.In(companyIdentifiers)).
		Scan(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("query companies: %v", err))
		return
	}

	usersByEmail := make(map[string]*control.User, len(users))
	for i := range users {
		usersByEmail[users[i].Email] = &users[i]
	}
	companiesByIdentifier := make(map[string]*control.Company, len(companies))
	for i := range companies {
		companiesByIdentifier[companies[i].Identifier] = &companies[i]
	}

	userIDs := make([]int64, 0, len(users))
	for _, u := range users {
		userIDs = append(userIDs, u.ID)
	}
	companyIDs := make([]int64, 0, len(companies))
	for _, c := range companies {
		companyIDs = append(companyIDs, c.ID)
	}

	if len(userIDs) > 0 && len(companyIDs) > 0 {
		if _, err := tx.NewDelete().Model((*control.Permission)(nil)).
			Where("user_id IN (?) AND company_id IN (?)", bun.In(userIDs), bun.In(companyIDs)).
			Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("delete permissions: %v", err))
			return
		}
	}

	for companyIdentifier, roles := range req.PermissionsByCompany {
		company, ok := companiesByIdentifier[companyIdentifier]
		if !ok {
			continue
		}
		for _, email := range req.Emails {
			user, ok := usersByEmail[email]
			if !ok {
				continue
			}
			for _, role := range roles {
				if role != control.RoleOperator && role != control.RolePayroll {
					continue
				}
				perm := &control.Permission{
					Identifier: crud.NewIdentifier(),
					UserID:     user.ID,
					CompanyID:  company.ID,
					Role:       role,
				}
				if _, err := tx.NewInsert().Model(perm).Exec(ctx); err != nil {
					response.InternalError(w, fmt.Sprintf("insert permission: %v", err))
					return
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		response.InternalError(w, fmt.Sprintf("commit: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"state":               "success",
		"users_processed":     len(users),
		"companies_processed": len(companies),
	})
}
