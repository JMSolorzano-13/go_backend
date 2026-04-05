package control

import (
	"time"

	"github.com/uptrace/bun"
)

type Param struct {
	bun.BaseModel `bun:"table:param,alias:par"`

	ID         int64      `bun:"id,pk,autoincrement" json:"id"`
	Identifier string     `bun:"identifier,type:uuid,notnull,unique" json:"identifier"`
	Name       string     `bun:"name,notnull" json:"name"`
	Value      *string    `bun:"value" json:"value"`
	CreatedAt  time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt  *time.Time `bun:"updated_at" json:"updated_at"`
}
