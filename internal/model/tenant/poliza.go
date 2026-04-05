package tenant

import (
	"time"

	"github.com/uptrace/bun"
)

type Poliza struct {
	bun.BaseModel `bun:"table:poliza"`

	Identifier    string     `bun:"identifier,pk,type:uuid" json:"identifier"`
	Fecha         time.Time  `bun:"fecha,notnull" json:"fecha"`
	Tipo          string     `bun:"tipo,notnull" json:"tipo"`
	Numero        string     `bun:"numero,notnull" json:"numero"`
	Concepto      *string    `bun:"concepto" json:"concepto"`
	SistemaOrigen *string    `bun:"sistema_origen" json:"sistema_origen"`
	CreatedAt     time.Time  `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt     *time.Time `bun:"updated_at" json:"updated_at"`
}

type PolizaCFDI struct {
	bun.BaseModel `bun:"table:poliza_cfdi"`

	PolizaIdentifier string     `bun:"poliza_identifier,pk,type:uuid" json:"poliza_identifier"`
	UUIDRelated      string     `bun:"uuid_related,pk,type:uuid" json:"uuid_related"`
	CreatedAt        time.Time  `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt        *time.Time `bun:"updated_at" json:"updated_at"`
}

type PolizaMovimiento struct {
	bun.BaseModel `bun:"table:poliza_movimiento"`

	Identifier       string     `bun:"identifier,pk,type:uuid" json:"identifier"`
	Numerador        *string    `bun:"numerador" json:"numerador"`
	CuentaContable   *string    `bun:"cuenta_contable" json:"cuenta_contable"`
	Nombre           *string    `bun:"nombre" json:"nombre"`
	Cargo            float64    `bun:"cargo,notnull" json:"cargo"`
	Abono            float64    `bun:"abono,notnull" json:"abono"`
	CargoME          float64    `bun:"cargo_me,notnull" json:"cargo_me"`
	AbonoME          float64    `bun:"abono_me,notnull" json:"abono_me"`
	Concepto         *string    `bun:"concepto" json:"concepto"`
	Referencia       *string    `bun:"referencia" json:"referencia"`
	PolizaIdentifier string     `bun:"poliza_identifier,notnull,type:uuid" json:"poliza_identifier"`
	CreatedAt        time.Time  `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt        *time.Time `bun:"updated_at" json:"updated_at"`
}
