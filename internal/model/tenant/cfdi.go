package tenant

import (
	"time"

	"github.com/uptrace/bun"
)

type CFDI struct {
	bun.BaseModel `bun:"table:cfdi"`

	CompanyIdentifier string    `bun:"company_identifier,pk,type:uuid" json:"company_identifier"`
	IsIssued          bool      `bun:"is_issued,pk" json:"is_issued"`
	UUID              string    `bun:"UUID,pk,type:uuid" json:"UUID"`

	Fecha                                time.Time  `bun:"Fecha,notnull" json:"Fecha"`
	Total                                float64    `bun:"Total,notnull" json:"Total"`
	Folio                                *string    `bun:"Folio" json:"Folio"`
	Serie                                *string    `bun:"Serie" json:"Serie"`
	NoCertificado                        *string    `bun:"NoCertificado" json:"NoCertificado"`
	Certificado                          *string    `bun:"Certificado" json:"Certificado"`
	TipoDeComprobante                    string     `bun:"TipoDeComprobante,notnull" json:"TipoDeComprobante"`
	LugarExpedicion                      *string    `bun:"LugarExpedicion" json:"LugarExpedicion"`
	FormaPago                            *string    `bun:"FormaPago" json:"FormaPago"`
	MetodoPago                           *string    `bun:"MetodoPago" json:"MetodoPago"`
	Moneda                               *string    `bun:"Moneda" json:"Moneda"`
	SubTotal                             *float64   `bun:"SubTotal" json:"SubTotal"`
	RfcEmisor                            string     `bun:"RfcEmisor,notnull" json:"RfcEmisor"`
	NombreEmisor                         *string    `bun:"NombreEmisor" json:"NombreEmisor"`
	RfcReceptor                          string     `bun:"RfcReceptor,notnull" json:"RfcReceptor"`
	NombreReceptor                       *string    `bun:"NombreReceptor" json:"NombreReceptor"`
	RfcPac                               *string    `bun:"RfcPac" json:"RfcPac"`
	FechaCertificacionSat                time.Time  `bun:"FechaCertificacionSat,notnull" json:"FechaCertificacionSat"`
	Estatus                              bool       `bun:"Estatus,notnull" json:"Estatus"`
	ExcludeFromIVA                       bool       `bun:"ExcludeFromIVA,notnull" json:"ExcludeFromIVA"`
	ExcludeFromISR                       bool       `bun:"ExcludeFromISR,notnull" json:"ExcludeFromISR"`
	FechaCancelacion                     *time.Time `bun:"FechaCancelacion" json:"FechaCancelacion"`
	TipoCambio                           *float64   `bun:"TipoCambio" json:"TipoCambio"`
	Conceptos                            *string    `bun:"Conceptos" json:"Conceptos"`
	Version                              *string    `bun:"Version" json:"Version"`
	Sello                                *string    `bun:"Sello" json:"Sello"`
	UsoCFDIReceptor                      *string    `bun:"UsoCFDIReceptor" json:"UsoCFDIReceptor"`
	RegimenFiscalEmisor                  *string    `bun:"RegimenFiscalEmisor" json:"RegimenFiscalEmisor"`
	CondicionesDePago                    *string    `bun:"CondicionesDePago" json:"CondicionesDePago"`
	CfdiRelacionados                     *string    `bun:"CfdiRelacionados" json:"CfdiRelacionados"`
	Neto                                 *float64   `bun:"Neto" json:"Neto"`
	TrasladosIVA                         *float64   `bun:"TrasladosIVA" json:"TrasladosIVA"`
	TrasladosIEPS                        *float64   `bun:"TrasladosIEPS" json:"TrasladosIEPS"`
	TrasladosISR                         *float64   `bun:"TrasladosISR" json:"TrasladosISR"`
	RetencionesIVA                       *float64   `bun:"RetencionesIVA" json:"RetencionesIVA"`
	RetencionesIEPS                      *float64   `bun:"RetencionesIEPS" json:"RetencionesIEPS"`
	RetencionesISR                       *float64   `bun:"RetencionesISR" json:"RetencionesISR"`
	FechaFiltro                          time.Time  `bun:"FechaFiltro,notnull" json:"FechaFiltro"`
	Impuestos                            *string    `bun:"Impuestos" json:"Impuestos"`
	Exportacion                          *string    `bun:"Exportacion" json:"Exportacion"`
	Periodicidad                         *string    `bun:"Periodicidad" json:"Periodicidad"`
	Meses                                *string    `bun:"Meses" json:"Meses"`
	Year                                 *string    `bun:"Year" json:"Year"`
	DomicilioFiscalReceptor              *string    `bun:"DomicilioFiscalReceptor" json:"DomicilioFiscalReceptor"`
	RegimenFiscalReceptor                *string    `bun:"RegimenFiscalReceptor" json:"RegimenFiscalReceptor"`
	TotalMXN                             *float64   `bun:"TotalMXN" json:"TotalMXN"`
	SubTotalMXN                          *float64   `bun:"SubTotalMXN" json:"SubTotalMXN"`
	NetoMXN                              *float64   `bun:"NetoMXN" json:"NetoMXN"`
	DescuentoMXN                         *float64   `bun:"DescuentoMXN" json:"DescuentoMXN"`
	TrasladosIVAMXN                      *float64   `bun:"TrasladosIVAMXN" json:"TrasladosIVAMXN"`
	TrasladosIEPSMXN                     *float64   `bun:"TrasladosIEPSMXN" json:"TrasladosIEPSMXN"`
	TrasladosISRMXN                      *float64   `bun:"TrasladosISRMXN" json:"TrasladosISRMXN"`
	RetencionesIVAMXN                    *float64   `bun:"RetencionesIVAMXN" json:"RetencionesIVAMXN"`
	RetencionesIEPSMXN                   *float64   `bun:"RetencionesIEPSMXN" json:"RetencionesIEPSMXN"`
	RetencionesISRMXN                    *float64   `bun:"RetencionesISRMXN" json:"RetencionesISRMXN"`
	NoCertificadoSAT                     *string    `bun:"NoCertificadoSAT" json:"NoCertificadoSAT"`
	SelloSAT                             *string    `bun:"SelloSAT" json:"SelloSAT"`
	Descuento                            *float64   `bun:"Descuento" json:"Descuento"`
	PaymentDate                          time.Time  `bun:"PaymentDate,notnull" json:"PaymentDate"`
	TipoDeComprobanteIMetodoPagoPPD      bool       `bun:"TipoDeComprobante_I_MetodoPago_PPD,notnull" json:"TipoDeComprobante_I_MetodoPago_PPD"`
	TipoDeComprobanteIMetodoPagoPUE      bool       `bun:"TipoDeComprobante_I_MetodoPago_PUE,notnull" json:"TipoDeComprobante_I_MetodoPago_PUE"`
	TipoDeComprobanteEMetodoPagoPPD      bool       `bun:"TipoDeComprobante_E_MetodoPago_PPD,notnull" json:"TipoDeComprobante_E_MetodoPago_PPD"`
	TipoDeComprobanteECfdiRelacionadosNone bool     `bun:"TipoDeComprobante_E_CfdiRelacionados_None,notnull" json:"TipoDeComprobante_E_CfdiRelacionados_None"`
	CancelledOtherMonth                  bool       `bun:"cancelled_other_month,notnull" json:"cancelled_other_month"`
	OtherRFC                             *string    `bun:"other_rfc" json:"other_rfc"`
	CreatedAt                            time.Time  `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt                            time.Time  `bun:"updated_at,notnull" json:"updated_at"`
	Active                               bool       `bun:"active,notnull" json:"active"`
	IsTooBig                             bool       `bun:"is_too_big,notnull" json:"is_too_big"`
	FromXML                              bool       `bun:"from_xml,notnull" json:"from_xml"`
	XMLContent                           *string    `bun:"xml_content,type:xml" json:"xml_content"`
	ADDExists                            bool       `bun:"add_exists,notnull" json:"add_exists"`
	ADDCancelDate                        *time.Time `bun:"add_cancel_date" json:"add_cancel_date"`
	BaseIVA16                            *float64   `bun:"BaseIVA16" json:"BaseIVA16"`
	BaseIVA8                             *float64   `bun:"BaseIVA8" json:"BaseIVA8"`
	BaseIVA0                             *float64   `bun:"BaseIVA0" json:"BaseIVA0"`
	BaseIVAExento                        *float64   `bun:"BaseIVAExento" json:"BaseIVAExento"`
	IVATrasladado16                      *float64   `bun:"IVATrasladado16" json:"IVATrasladado16"`
	IVATrasladado8                       *float64   `bun:"IVATrasladado8" json:"IVATrasladado8"`
	PRCount                              float64    `bun:"pr_count,notnull" json:"pr_count"`
}
