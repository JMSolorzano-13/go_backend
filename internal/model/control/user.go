package control

import (
	"time"

	"github.com/uptrace/bun"
)

type User struct {
	// Alias must match domain paths from the API (e.g. user.email on Permission/search).
	bun.BaseModel `bun:"table:user,alias:user"`

	ID                           int64      `bun:"id,pk,autoincrement" json:"id"`
	Identifier                   string     `bun:"identifier,type:uuid,notnull,unique" json:"identifier"`
	Name                         *string    `bun:"name" json:"name"`
	Email                        string     `bun:"email,notnull" json:"email"`
	CognitoSub                   *string    `bun:"cognito_sub,unique" json:"cognito_sub"`
	InvitedByID                  *int64     `bun:"invited_by_id" json:"invited_by_id"`
	SourceName                   *string    `bun:"source_name" json:"source_name"`
	Phone                        *string    `bun:"phone" json:"phone"`
	OdooIdentifier               *int64     `bun:"odoo_identifier" json:"odoo_identifier"`
	StripeIdentifier             *string    `bun:"stripe_identifier" json:"stripe_identifier"`
	StripeSubscriptionIdentifier *string    `bun:"stripe_subscription_identifier" json:"stripe_subscription_identifier"`
	CreatedAt                    time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt                    *time.Time `bun:"updated_at" json:"updated_at"`
}
