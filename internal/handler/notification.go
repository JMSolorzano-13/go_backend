package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Notification struct {
	cfg      *config.Config
	database *db.Database
}

func NewNotification(cfg *config.Config, database *db.Database) *Notification {
	return &Notification{cfg: cfg, database: database}
}

var notificationMeta = crud.ModelMeta{
	DefaultOrderBy: "id ASC",
}

// Search handles POST /api/Notification/config/search — no auth required.
func (h *Notification) Search(w http.ResponseWriter, r *http.Request) {
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
	result, err := crud.Search[control.NotificationConfig](r.Context(), h.database.Replica, params, notificationMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, result)
}

// SetConfig handles PUT /api/Notification/config — updates notification types for a workspace.
//
// Python source: NotificationConfigController.set_notification_types(notifications_by_user, workspace).
// Body: {workspace_id: str, notifications: {type: [emails]}}
// For each notification type, resolves user emails, diffs against current configs,
// deletes removed and inserts added notification_config rows.
// Returns the final set of configs for the last processed user.
func (h *Notification) SetConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var req struct {
		WorkspaceID   string              `json:"workspace_id"`
		Notifications map[string][]string `json:"notifications"` // notif_type -> [emails]
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if req.WorkspaceID == "" {
		response.BadRequest(w, "workspace_id is required")
		return
	}

	ctx := r.Context()
	currentUser, _ := auth.UserFromContext(ctx)

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var workspace control.Workspace
	if err := database.Primary.NewSelect().Model(&workspace).
		Where("identifier = ?", req.WorkspaceID).
		Scan(ctx); err != nil {
		response.NotFound(w, fmt.Sprintf("workspace %s not found", req.WorkspaceID))
		return
	}

	validTypes := map[string]bool{
		control.NotificationTypeError:    true,
		control.NotificationTypeEFOS:     true,
		control.NotificationTypeCanceled: true,
	}

	// Build inverted map: email -> set of notification types.
	emailToTypes := make(map[string]map[string]bool)
	for notifType, emails := range req.Notifications {
		if !validTypes[notifType] {
			response.BadRequest(w, fmt.Sprintf("invalid notification type: %s", notifType))
			return
		}
		for _, email := range emails {
			if emailToTypes[email] == nil {
				emailToTypes[email] = make(map[string]bool)
			}
			emailToTypes[email][notifType] = true
		}
	}

	tx, err := database.Primary.BeginTx(ctx, nil)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("begin tx: %v", err))
		return
	}
	defer tx.Rollback()

	var lastUserID int64
	for email, desiredTypes := range emailToTypes {
		var user control.User
		if err := tx.NewSelect().Model(&user).
			Where("email = ?", email).
			Scan(ctx); err != nil {
			continue
		}
		lastUserID = user.ID

		var existingConfigs []control.NotificationConfig
		if err := tx.NewSelect().Model(&existingConfigs).
			Where("user_id = ? AND workspace_id = ?", user.ID, workspace.ID).
			Scan(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("query configs: %v", err))
			return
		}

		currentTypes := make(map[string]bool)
		for _, c := range existingConfigs {
			currentTypes[c.NotificationType] = true
		}

		// Add new types.
		for notifType := range desiredTypes {
			if !currentTypes[notifType] {
				config := &control.NotificationConfig{
					Identifier:      crud.NewIdentifier(),
					UserID:          user.ID,
					WorkspaceID:     workspace.ID,
					NotificationType: notifType,
				}
				if _, err := tx.NewInsert().Model(config).Exec(ctx); err != nil {
					response.InternalError(w, fmt.Sprintf("insert notification config: %v", err))
					return
				}
			}
		}

		// Remove obsolete types.
		toRemove := make([]string, 0)
		for notifType := range currentTypes {
			if !desiredTypes[notifType] {
				toRemove = append(toRemove, notifType)
			}
		}
		if len(toRemove) > 0 {
			if _, err := tx.NewDelete().Model((*control.NotificationConfig)(nil)).
				Where("user_id = ? AND workspace_id = ? AND notification_type IN (?)",
					user.ID, workspace.ID, bun.In(toRemove)).
				Exec(ctx); err != nil {
				response.InternalError(w, fmt.Sprintf("delete notification configs: %v", err))
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		response.InternalError(w, fmt.Sprintf("commit: %v", err))
		return
	}

	_ = currentUser

	var finalConfigs []control.NotificationConfig
	if lastUserID > 0 {
		_ = h.database.Replica.NewSelect().Model(&finalConfigs).
			Where("user_id = ? AND workspace_id = ?", lastUserID, workspace.ID).
			Scan(ctx)
	}

	result := crud.Serialize(finalConfigs)
	response.WriteJSON(w, http.StatusOK, result)
}
