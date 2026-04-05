package tenant

import (
	"time"

	"github.com/uptrace/bun"
)

type CfdiRelacionado struct {
	bun.BaseModel `bun:"table:cfdi_relation"`

	Identifier        string     `bun:"identifier,pk,type:uuid" json:"identifier"`
	CompanyIdentifier string     `bun:"company_identifier,pk,type:uuid" json:"company_identifier"`
	IsIssued          bool       `bun:"is_issued,pk" json:"is_issued"`
	CreatedAt         *time.Time `bun:"created_at" json:"created_at"`
	UUIDOrigin        string     `bun:"uuid_origin,notnull,type:uuid" json:"uuid_origin"`
	TipoDeComprobante string    `bun:"TipoDeComprobante,notnull" json:"TipoDeComprobante"`
	Estatus           bool       `bun:"Estatus,notnull" json:"Estatus"`
	UUIDRelated       string     `bun:"uuid_related,notnull,type:uuid" json:"uuid_related"`
	TipoRelacion      string     `bun:"TipoRelacion,notnull" json:"TipoRelacion"`
	UpdatedAt         *time.Time `bun:"updated_at" json:"updated_at"`
}
