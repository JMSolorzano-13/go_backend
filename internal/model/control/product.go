package control

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type Product struct {
	bun.BaseModel `bun:"table:product,alias:prod"`

	StripeIdentifier      string          `bun:"stripe_identifier,pk,notnull" json:"stripe_identifier"`
	Characteristics       json.RawMessage `bun:"characteristics,type:json,notnull" json:"characteristics"`
	Price                 int64           `bun:"price,notnull" json:"price"`
	StripePriceIdentifier string          `bun:"stripe_price_identifier,notnull" json:"stripe_price_identifier"`
	StripeName            string          `bun:"stripe_name,notnull" json:"stripe_name"`
	CreatedAt             time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt             *time.Time      `bun:"updated_at" json:"updated_at"`
}
