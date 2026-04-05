package tenant

import (
	"time"

	"github.com/uptrace/bun"
)

type Payment struct {
	bun.BaseModel `bun:"table:payment"`

	Identifier        string     `bun:"identifier,pk,type:uuid" json:"identifier"`
	CompanyIdentifier string     `bun:"company_identifier,pk,type:uuid" json:"company_identifier"`
	IsIssued          bool       `bun:"is_issued,notnull" json:"is_issued"`
	Estatus           bool       `bun:"Estatus,notnull" json:"Estatus"`
	UUIDOrigin        string     `bun:"uuid_origin,notnull,type:uuid" json:"uuid_origin"`
	Index             int64      `bun:"index,notnull" json:"index"`
	FechaPago         time.Time  `bun:"FechaPago,notnull" json:"FechaPago"`
	FormaDePagoP      string     `bun:"FormaDePagoP,notnull" json:"FormaDePagoP"`
	MonedaP           string     `bun:"MonedaP,notnull" json:"MonedaP"`
	Monto             float64    `bun:"Monto,notnull" json:"Monto"`
	TipoCambioP       *float64   `bun:"TipoCambioP" json:"TipoCambioP"`
	NumOperacion      *string    `bun:"NumOperacion" json:"NumOperacion"`
	RfcEmisorCtaOrd   *string    `bun:"RfcEmisorCtaOrd" json:"RfcEmisorCtaOrd"`
	NomBancoOrdExt    *string    `bun:"NomBancoOrdExt" json:"NomBancoOrdExt"`
	CtaOrdenante      *string    `bun:"CtaOrdenante" json:"CtaOrdenante"`
	RfcEmisorCtaBen   *string    `bun:"RfcEmisorCtaBen" json:"RfcEmisorCtaBen"`
	CtaBeneficiario   *string    `bun:"CtaBeneficiario" json:"CtaBeneficiario"`
	TipoCadPago       *string    `bun:"TipoCadPago" json:"TipoCadPago"`
	CertPago          *string    `bun:"CertPago" json:"CertPago"`
	CadPago           *string    `bun:"CadPago" json:"CadPago"`
	SelloPago         *string    `bun:"SelloPago" json:"SelloPago"`
	CreatedAt         time.Time  `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt         *time.Time `bun:"updated_at" json:"updated_at"`
}
