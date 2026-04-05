package tenant

import (
	"time"

	"github.com/uptrace/bun"
)

type Nomina struct {
	bun.BaseModel `bun:"table:nomina"`

	CompanyIdentifier                    string     `bun:"company_identifier,pk,type:uuid" json:"company_identifier"`
	CfdiUUID                             string     `bun:"cfdi_uuid,pk,type:uuid,unique" json:"cfdi_uuid"`
	Version                              string     `bun:"Version,notnull" json:"Version"`
	TipoNomina                           string     `bun:"TipoNomina,notnull" json:"TipoNomina"`
	FechaPago                            time.Time  `bun:"FechaPago,notnull" json:"FechaPago"`
	FechaInicialPago                     time.Time  `bun:"FechaInicialPago,notnull" json:"FechaInicialPago"`
	FechaFinalPago                       time.Time  `bun:"FechaFinalPago,notnull" json:"FechaFinalPago"`
	NumDiasPagados                       float64    `bun:"NumDiasPagados,notnull" json:"NumDiasPagados"`
	TotalPercepciones                    *float64   `bun:"TotalPercepciones" json:"TotalPercepciones"`
	TotalDeducciones                     *float64   `bun:"TotalDeducciones" json:"TotalDeducciones"`
	TotalOtrosPagos                      *float64   `bun:"TotalOtrosPagos" json:"TotalOtrosPagos"`
	EmisorRegistroPatronal               *string    `bun:"EmisorRegistroPatronal" json:"EmisorRegistroPatronal"`
	ReceptorCurp                         string     `bun:"ReceptorCurp,notnull" json:"ReceptorCurp"`
	ReceptorNumSeguridadSocial           *string    `bun:"ReceptorNumSeguridadSocial" json:"ReceptorNumSeguridadSocial"`
	ReceptorFechaInicioRelLaboral        *time.Time `bun:"ReceptorFechaInicioRelLaboral" json:"ReceptorFechaInicioRelLaboral"`
	ReceptorAntiguedad                   *string    `bun:"ReceptorAntigüedad" json:"ReceptorAntigüedad"`
	ReceptorTipoContrato                 string     `bun:"ReceptorTipoContrato,notnull" json:"ReceptorTipoContrato"`
	ReceptorSindicalizado                *bool      `bun:"ReceptorSindicalizado" json:"ReceptorSindicalizado"`
	ReceptorTipoJornada                  *string    `bun:"ReceptorTipoJornada" json:"ReceptorTipoJornada"`
	ReceptorTipoRegimen                  string     `bun:"ReceptorTipoRegimen,notnull" json:"ReceptorTipoRegimen"`
	ReceptorNumEmpleado                  string     `bun:"ReceptorNumEmpleado,notnull" json:"ReceptorNumEmpleado"`
	ReceptorDepartamento                 *string    `bun:"ReceptorDepartamento" json:"ReceptorDepartamento"`
	ReceptorPuesto                       *string    `bun:"ReceptorPuesto" json:"ReceptorPuesto"`
	ReceptorRiesgoPuesto                 *string    `bun:"ReceptorRiesgoPuesto" json:"ReceptorRiesgoPuesto"`
	ReceptorPeriodicidadPago             string     `bun:"ReceptorPeriodicidadPago,notnull" json:"ReceptorPeriodicidadPago"`
	ReceptorBanco                        *string    `bun:"ReceptorBanco" json:"ReceptorBanco"`
	ReceptorCuentaBancaria               *string    `bun:"ReceptorCuentaBancaria" json:"ReceptorCuentaBancaria"`
	ReceptorSalarioBaseCotApor           *float64   `bun:"ReceptorSalarioBaseCotApor" json:"ReceptorSalarioBaseCotApor"`
	ReceptorSalarioDiarioIntegrado       *float64   `bun:"ReceptorSalarioDiarioIntegrado" json:"ReceptorSalarioDiarioIntegrado"`
	ReceptorClaveEntFed                  string     `bun:"ReceptorClaveEntFed,notnull" json:"ReceptorClaveEntFed"`
	PercepcionesTotalSueldos             *float64   `bun:"PercepcionesTotalSueldos" json:"PercepcionesTotalSueldos"`
	PercepcionesTotalGravado             *float64   `bun:"PercepcionesTotalGravado" json:"PercepcionesTotalGravado"`
	PercepcionesTotalExento              *float64   `bun:"PercepcionesTotalExento" json:"PercepcionesTotalExento"`
	PercepcionesSeparacionIndemnizacion  *float64   `bun:"PercepcionesSeparacionIndemnizacion" json:"PercepcionesSeparacionIndemnizacion"`
	PercepcionesJubilacionPensionRetiro  *float64   `bun:"PercepcionesJubilacionPensionRetiro" json:"PercepcionesJubilacionPensionRetiro"`
	DeduccionesTotalOtrasDeducciones     *float64   `bun:"DeduccionesTotalOtrasDeducciones" json:"DeduccionesTotalOtrasDeducciones"`
	DeduccionesTotalImpuestosRetenidos   *float64   `bun:"DeduccionesTotalImpuestosRetenidos" json:"DeduccionesTotalImpuestosRetenidos"`
	SubsidioCausado                      *float64   `bun:"SubsidioCausado" json:"SubsidioCausado"`
	AjusteISRRetenido                    *float64   `bun:"AjusteISRRetenido" json:"AjusteISRRetenido"`
}
