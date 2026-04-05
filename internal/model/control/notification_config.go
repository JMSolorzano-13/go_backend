package control

import (
	"time"

	"github.com/uptrace/bun"
)

const (
	NotificationTypeError    = "ERROR"
	NotificationTypeEFOS     = "EFOS"
	NotificationTypeCanceled = "CANCELED"
)

type NotificationConfig struct {
	bun.BaseModel `bun:"table:notification_config,alias:nc"`

	ID                  int64      `bun:"id,pk,autoincrement" json:"id"`
	Identifier          string     `bun:"identifier,type:uuid,notnull,unique" json:"identifier"`
	UserID              int64      `bun:"user_id,notnull" json:"user_id"`
	WorkspaceID         int64      `bun:"workspace_id,notnull" json:"workspace_id"`
	WorkspaceIdentifier *string    `bun:"workspace_identifier,type:uuid" json:"workspace_identifier"`
	NotificationType    string     `bun:"notification_type,notnull" json:"notification_type"`
	CreatedAt           time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt           *time.Time `bun:"updated_at" json:"updated_at"`

	User      *User      `bun:"rel:belongs-to,join:user_id=id" json:"user,omitempty"`
	Workspace *Workspace `bun:"rel:belongs-to,join:workspace_identifier=identifier" json:"workspace,omitempty"`
}
