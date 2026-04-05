package control

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type Workspace struct {
	bun.BaseModel `bun:"table:workspace,alias:w"`

	ID                int64           `bun:"id,pk,autoincrement" json:"id"`
	Identifier        string          `bun:"identifier,type:uuid,notnull,unique" json:"identifier"`
	Name              *string         `bun:"name" json:"name"`
	OwnerID           *int64          `bun:"owner_id" json:"owner_id"`
	License           json.RawMessage `bun:"license,type:jsonb,notnull" json:"license"`
	ValidUntil        *time.Time      `bun:"valid_until" json:"valid_until"`
	OdooID            *int64          `bun:"odoo_id" json:"odoo_id"`
	StripeStatus      *string         `bun:"stripe_status" json:"stripe_status"`
	PastoWorkerID     *string         `bun:"pasto_worker_id,unique" json:"pasto_worker_id"`
	PastoLicenseKey   *string         `bun:"pasto_license_key,unique" json:"pasto_license_key"`
	PastoInstalled    *bool           `bun:"pasto_installed" json:"pasto_installed"`
	PastoWorkerToken  *string         `bun:"pasto_worker_token" json:"pasto_worker_token"`
	ADDPermission     *bool           `bun:"add_permission,default:false" json:"add_permission"`
	CreatedAt         time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt         *time.Time      `bun:"updated_at" json:"updated_at"`

	Owner *User `bun:"rel:belongs-to,join:owner_id=id" json:"owner,omitempty"`
}
