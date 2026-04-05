package tenant

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type DoctoRelacionado struct {
	bun.BaseModel `bun:"table:payment_relation"`

	Identifier        string          `bun:"identifier,pk,type:uuid" json:"identifier"`
	CompanyIdentifier string          `bun:"company_identifier,pk,type:uuid" json:"company_identifier"`
	IsIssued          bool            `bun:"is_issued,notnull" json:"is_issued"`
	CreatedAt         *time.Time      `bun:"created_at" json:"created_at"`
	PaymentIdentifier string          `bun:"payment_identifier,notnull,type:uuid" json:"payment_identifier"`
	UUID              string          `bun:"UUID,notnull,type:uuid" json:"UUID"`
	FechaPago         time.Time       `bun:"FechaPago,notnull" json:"FechaPago"`
	UUIDRelated       string          `bun:"UUID_related,notnull,type:uuid" json:"UUID_related"`
	Serie             *string         `bun:"Serie" json:"Serie"`
	Folio             *string         `bun:"Folio" json:"Folio"`
	MonedaDR          string          `bun:"MonedaDR,notnull" json:"MonedaDR"`
	EquivalenciaDR    *float64        `bun:"EquivalenciaDR" json:"EquivalenciaDR"`
	MetodoDePagoDR    *string         `bun:"MetodoDePagoDR" json:"MetodoDePagoDR"`
	NumParcialidad    int64           `bun:"NumParcialidad,notnull" json:"NumParcialidad"`
	ImpSaldoAnt       float64         `bun:"ImpSaldoAnt,notnull" json:"ImpSaldoAnt"`
	ImpPagado         float64         `bun:"ImpPagado,notnull" json:"ImpPagado"`
	ImpPagadoMXN      float64         `bun:"ImpPagadoMXN,notnull" json:"ImpPagadoMXN"`
	ImpSaldoInsoluto  float64         `bun:"ImpSaldoInsoluto,notnull" json:"ImpSaldoInsoluto"`
	Active            bool            `bun:"active,notnull" json:"active"`
	Applied           bool            `bun:"applied,notnull" json:"applied"`
	ObjetoImpDR       *string         `bun:"ObjetoImpDR" json:"ObjetoImpDR"`
	BaseIVA16         float64         `bun:"BaseIVA16,notnull" json:"BaseIVA16"`
	BaseIVA8          float64         `bun:"BaseIVA8,notnull" json:"BaseIVA8"`
	BaseIVA0          float64         `bun:"BaseIVA0,notnull" json:"BaseIVA0"`
	BaseIVAExento     float64         `bun:"BaseIVAExento,notnull" json:"BaseIVAExento"`
	IVATrasladado16   float64         `bun:"IVATrasladado16,notnull" json:"IVATrasladado16"`
	IVATrasladado8    float64         `bun:"IVATrasladado8,notnull" json:"IVATrasladado8"`
	TrasladosIVAMXN   float64         `bun:"TrasladosIVAMXN,notnull" json:"TrasladosIVAMXN"`
	RetencionesIVAMXN *float64        `bun:"RetencionesIVAMXN" json:"RetencionesIVAMXN"`
	RetencionesDR     json.RawMessage `bun:"RetencionesDR,type:jsonb" json:"RetencionesDR"`
	TrasladosDR       json.RawMessage `bun:"TrasladosDR,type:jsonb" json:"TrasladosDR"`
	Estatus           bool            `bun:"Estatus,notnull" json:"Estatus"`
	ExcludeFromIVA    bool            `bun:"ExcludeFromIVA,notnull" json:"ExcludeFromIVA"`
	ExcludeFromISR    bool            `bun:"ExcludeFromISR,notnull" json:"ExcludeFromISR"`
	UpdatedAt         *time.Time      `bun:"updated_at" json:"updated_at"`
}
