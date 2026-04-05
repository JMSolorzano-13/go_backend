package control

import (
	"time"

	"github.com/uptrace/bun"
)

const (
	RoleOperator = "OPERATOR"
	RolePayroll  = "PAYROLL"
)

type Permission struct {
	bun.BaseModel `bun:"table:permission,alias:p"`

	ID         int64      `bun:"id,pk,autoincrement" json:"id"`
	Identifier string     `bun:"identifier,type:uuid,notnull,unique" json:"identifier"`
	UserID     int64      `bun:"user_id,notnull" json:"user_id"`
	CompanyID  int64      `bun:"company_id,notnull" json:"company_id"`
	Role       string     `bun:"role,notnull" json:"role"`
	CreatedAt  time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt  *time.Time `bun:"updated_at" json:"updated_at"`

	User    *User    `bun:"rel:belongs-to,join:user_id=id" json:"user,omitempty"`
	Company *Company `bun:"rel:belongs-to,join:company_id=id" json:"company,omitempty"`
}
