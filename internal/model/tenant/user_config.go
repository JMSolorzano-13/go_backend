package tenant

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type UserConfig struct {
	bun.BaseModel `bun:"table:user_config"`

	UserIdentifier string          `bun:"user_identifier,pk,type:uuid" json:"user_identifier"`
	Data           json.RawMessage `bun:"data,type:json,notnull" json:"data"`
	CreatedAt      *time.Time      `bun:"created_at" json:"created_at"`
	UpdatedAt      *time.Time      `bun:"updated_at" json:"updated_at"`
}
