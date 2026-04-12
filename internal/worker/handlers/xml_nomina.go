package handlers

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// insertNomina parses the Complemento/Nomina element and upserts a Nomina row.
// Runs for any CFDI that contains a Nomina complemento (typically TipoDeComprobante="N").
//
// Mirrors Python XMLProcessor.create_nomina + NominaParser.parse.
// Uses ON CONFLICT DO NOTHING since (company_identifier, cfdi_uuid) is unique.
// Errors are logged and never fail the parent CFDI processing.
func (h *ProcessXML) insertNomina(
	ctx context.Context,
	conn bun.Conn,
	data *cfdiXMLData,
	companyID, xmlContent string,
	now time.Time,
	logger *slog.Logger,
) {
	n, err := parseNominaComplemento(xmlContent)
	if err != nil {
		logger.Debug("parse Nomina complemento failed", "uuid", data.UUID, "error", err)
		return
	}
	if n == nil {
		return
	}

	// Sindicalizado: "Sí" / "Si" → true, anything else → false or nil if absent.
	var sindicalizado *bool
	if strings.TrimSpace(n.Receptor.Sindicalizado) != "" {
		v := strings.ToLower(strings.TrimSpace(n.Receptor.Sindicalizado))
		b := v == "sí" || v == "si" || v == "true" || v == "1"
		sindicalizado = &b
	}

	// FechaInicioRelLaboral is optional.
	var fechaInicioRelLaboral *time.Time
	if t := parseDatetime(n.Receptor.FechaInicioRelLaboral); !t.IsZero() {
		fechaInicioRelLaboral = &t
	}

	// Optional float fields from Percepciones totals.
	optF := func(f float64) *float64 {
		if f == 0 {
			return nil
		}
		return &f
	}

	row := &tenant.Nomina{
		CompanyIdentifier:                   companyID,
		CfdiUUID:                            data.UUID,
		Version:                             n.Version,
		TipoNomina:                          n.TipoNomina,
		FechaPago:                           parseDatetime(n.FechaPago),
		FechaInicialPago:                    parseDatetime(n.FechaInicialPago),
		FechaFinalPago:                      parseDatetime(n.FechaFinalPago),
		NumDiasPagados:                      parseFloatStr(n.NumDiasPagados),
		TotalPercepciones:                   optF(parseFloatStr(n.TotalPercepciones)),
		TotalDeducciones:                    optF(parseFloatStr(n.TotalDeducciones)),
		TotalOtrosPagos:                     optF(parseFloatStr(n.TotalOtrosPagos)),
		EmisorRegistroPatronal:              nilIfEmpty(n.Emisor.RegistroPatronal),
		ReceptorCurp:                        strings.TrimSpace(n.Receptor.Curp),
		ReceptorNumSeguridadSocial:          nilIfEmpty(n.Receptor.NumSeguridadSocial),
		ReceptorFechaInicioRelLaboral:       fechaInicioRelLaboral,
		ReceptorAntiguedad:                  nilIfEmpty(n.Receptor.Antiguedad),
		ReceptorTipoContrato:                n.Receptor.TipoContrato,
		ReceptorSindicalizado:               sindicalizado,
		ReceptorTipoJornada:                 nilIfEmpty(n.Receptor.TipoJornada),
		ReceptorTipoRegimen:                 n.Receptor.TipoRegimen,
		ReceptorNumEmpleado:                 n.Receptor.NumEmpleado,
		ReceptorDepartamento:                nilIfEmpty(n.Receptor.Departamento),
		ReceptorPuesto:                      nilIfEmpty(n.Receptor.Puesto),
		ReceptorRiesgoPuesto:                nilIfEmpty(n.Receptor.RiesgoPuesto),
		ReceptorPeriodicidadPago:            n.Receptor.PeriodicidadPago,
		ReceptorBanco:                       nilIfEmpty(n.Receptor.Banco),
		ReceptorCuentaBancaria:              nilIfEmpty(n.Receptor.CuentaBancaria),
		ReceptorSalarioBaseCotApor:          optF(parseFloatStr(n.Receptor.SalarioBaseCotApor)),
		ReceptorSalarioDiarioIntegrado:      optF(parseFloatStr(n.Receptor.SalarioDiarioIntegrado)),
		ReceptorClaveEntFed:                 n.Receptor.ClaveEntFed,
		PercepcionesTotalSueldos:            optF(parseFloatStr(n.Percepciones.TotalSueldos)),
		PercepcionesTotalGravado:            optF(parseFloatStr(n.Percepciones.TotalGravado)),
		PercepcionesTotalExento:             optF(parseFloatStr(n.Percepciones.TotalExento)),
		PercepcionesSeparacionIndemnizacion: optF(parseFloatStr(n.Percepciones.TotalSeparacionIndemnizacion)),
		PercepcionesJubilacionPensionRetiro: optF(parseFloatStr(n.Percepciones.TotalJubilacionPensionRetiro)),
		DeduccionesTotalOtrasDeducciones:    optF(parseFloatStr(n.Deducciones.TotalOtrasDeducciones)),
		DeduccionesTotalImpuestosRetenidos:  optF(parseFloatStr(n.Deducciones.TotalImpuestosRetenidos)),
		SubsidioCausado:                     optF(n.SubsidioCausado),
		AjusteISRRetenido:                   optF(n.AjusteISRRetenido),
	}

	// Use a deterministic identifier for idempotency.
	row.CompanyIdentifier = companyID
	row.CfdiUUID = uuid.NewSHA1(uuid.NameSpaceURL,
		[]byte(fmt.Sprintf("nomina:%s:%s", companyID, data.UUID)),
	).String()
	// Restore the actual UUID — the model PK for nomina is (company_identifier, cfdi_uuid).
	// cfdi_uuid must match the CFDI UUID, not a generated one.
	row.CfdiUUID = data.UUID

	if _, err := conn.NewInsert().
		Model(row).
		On("CONFLICT (company_identifier, cfdi_uuid) DO NOTHING").
		Exec(ctx); err != nil {
		logger.Error("insert nomina failed", "uuid", data.UUID, "error", err)
	}
}

// ─── Nomina parsing ──────────────────────────────────────────────────────────

type nominaParsed struct {
	Version           string
	TipoNomina        string
	FechaPago         string
	FechaInicialPago  string
	FechaFinalPago    string
	NumDiasPagados    string
	TotalPercepciones string
	TotalDeducciones  string
	TotalOtrosPagos   string
	Emisor            struct {
		RegistroPatronal string
	}
	Receptor struct {
		Curp                   string
		NumSeguridadSocial     string
		FechaInicioRelLaboral  string
		Antiguedad             string
		TipoContrato           string
		Sindicalizado          string
		TipoJornada            string
		TipoRegimen            string
		NumEmpleado            string
		Departamento           string
		Puesto                 string
		RiesgoPuesto           string
		PeriodicidadPago       string
		Banco                  string
		CuentaBancaria         string
		SalarioBaseCotApor     string
		SalarioDiarioIntegrado string
		ClaveEntFed            string
	}
	Percepciones struct {
		TotalSueldos                  string
		TotalSeparacionIndemnizacion  string
		TotalJubilacionPensionRetiro  string
		TotalGravado                  string
		TotalExento                   string
	}
	Deducciones struct {
		TotalOtrasDeducciones    string
		TotalImpuestosRetenidos  string
	}
	// Computed from OtrosPagos/OtroPago[TipoOtroPago=002]/SubsidioAlEmpleo/@SubsidioCausado
	SubsidioCausado float64
	// Computed from Deducciones/Deduccion[TipoDeduccion=007] total Importe
	AjusteISRRetenido float64
}

// parseNominaComplemento extracts the Complemento/Nomina element from CFDI XML.
// Matches both Nomina v1.1 and v1.2 via local name (namespace-agnostic).
func parseNominaComplemento(xmlContent string) (*nominaParsed, error) {
	// SubsidioAlEmpleo inside OtroPago
	type SubsidioAlEmpleo struct {
		SubsidioCausado string `xml:"SubsidioCausado,attr"`
	}
	type OtroPago struct {
		TipoOtroPago      string            `xml:"TipoOtroPago,attr"`
		SubsidioAlEmpleo  *SubsidioAlEmpleo `xml:"SubsidioAlEmpleo"`
	}
	type OtrosPagos struct {
		OtroPago []OtroPago `xml:"OtroPago"`
	}
	type Deduccion struct {
		TipoDeduccion string `xml:"TipoDeduccion,attr"`
		Importe       string `xml:"Importe,attr"`
	}
	type Deducciones struct {
		TotalOtrasDeducciones   string      `xml:"TotalOtrasDeducciones,attr"`
		TotalImpuestosRetenidos string      `xml:"TotalImpuestosRetenidos,attr"`
		Deduccion               []Deduccion `xml:"Deduccion"`
	}
	type Percepciones struct {
		TotalSueldos                 string `xml:"TotalSueldos,attr"`
		TotalSeparacionIndemnizacion string `xml:"TotalSeparacionIndemnizacion,attr"`
		TotalJubilacionPensionRetiro string `xml:"TotalJubilacionPensionRetiro,attr"`
		TotalGravado                 string `xml:"TotalGravado,attr"`
		TotalExento                  string `xml:"TotalExento,attr"`
	}
	type Receptor struct {
		Curp                   string `xml:"Curp,attr"`
		NumSeguridadSocial     string `xml:"NumSeguridadSocial,attr"`
		FechaInicioRelLaboral  string `xml:"FechaInicioRelLaboral,attr"`
		Antiguedad             string `xml:"Antigüedad,attr"` // SAT uses ü
		TipoContrato           string `xml:"TipoContrato,attr"`
		Sindicalizado          string `xml:"Sindicalizado,attr"`
		TipoJornada            string `xml:"TipoJornada,attr"`
		TipoRegimen            string `xml:"TipoRegimen,attr"`
		NumEmpleado            string `xml:"NumEmpleado,attr"`
		Departamento           string `xml:"Departamento,attr"`
		Puesto                 string `xml:"Puesto,attr"`
		RiesgoPuesto           string `xml:"RiesgoPuesto,attr"`
		PeriodicidadPago       string `xml:"PeriodicidadPago,attr"`
		Banco                  string `xml:"Banco,attr"`
		CuentaBancaria         string `xml:"CuentaBancaria,attr"`
		SalarioBaseCotApor     string `xml:"SalarioBaseCotApor,attr"`
		SalarioDiarioIntegrado string `xml:"SalarioDiarioIntegrado,attr"`
		ClaveEntFed            string `xml:"ClaveEntFed,attr"`
	}
	type Emisor struct {
		RegistroPatronal string `xml:"RegistroPatronal,attr"`
	}
	type Nomina struct {
		Version           string       `xml:"Version,attr"`
		TipoNomina        string       `xml:"TipoNomina,attr"`
		FechaPago         string       `xml:"FechaPago,attr"`
		FechaInicialPago  string       `xml:"FechaInicialPago,attr"`
		FechaFinalPago    string       `xml:"FechaFinalPago,attr"`
		NumDiasPagados    string       `xml:"NumDiasPagados,attr"`
		TotalPercepciones string       `xml:"TotalPercepciones,attr"`
		TotalDeducciones  string       `xml:"TotalDeducciones,attr"`
		TotalOtrosPagos   string       `xml:"TotalOtrosPagos,attr"`
		Emisor            *Emisor      `xml:"Emisor"`
		Receptor          *Receptor    `xml:"Receptor"`
		Percepciones      *Percepciones `xml:"Percepciones"`
		Deducciones       *Deducciones `xml:"Deducciones"`
		OtrosPagos        *OtrosPagos  `xml:"OtrosPagos"`
	}
	type Complemento struct {
		Nomina *Nomina `xml:"Nomina"`
	}
	type Comprobante struct {
		XMLName     xml.Name    `xml:"Comprobante"`
		Complemento Complemento `xml:"Complemento"`
	}

	var comp Comprobante
	if err := xml.Unmarshal([]byte(xmlContent), &comp); err != nil {
		return nil, fmt.Errorf("unmarshal for Nomina: %w", err)
	}
	n := comp.Complemento.Nomina
	if n == nil {
		return nil, nil
	}

	out := &nominaParsed{
		Version:           n.Version,
		TipoNomina:        n.TipoNomina,
		FechaPago:         strings.TrimSpace(n.FechaPago),
		FechaInicialPago:  strings.TrimSpace(n.FechaInicialPago),
		FechaFinalPago:    strings.TrimSpace(n.FechaFinalPago),
		NumDiasPagados:    n.NumDiasPagados,
		TotalPercepciones: n.TotalPercepciones,
		TotalDeducciones:  n.TotalDeducciones,
		TotalOtrosPagos:   n.TotalOtrosPagos,
	}
	if n.Emisor != nil {
		out.Emisor.RegistroPatronal = n.Emisor.RegistroPatronal
	}
	if n.Receptor != nil {
		r := n.Receptor
		out.Receptor.Curp = r.Curp
		out.Receptor.NumSeguridadSocial = r.NumSeguridadSocial
		out.Receptor.FechaInicioRelLaboral = r.FechaInicioRelLaboral
		out.Receptor.Antiguedad = r.Antiguedad
		out.Receptor.TipoContrato = r.TipoContrato
		out.Receptor.Sindicalizado = r.Sindicalizado
		out.Receptor.TipoJornada = r.TipoJornada
		out.Receptor.TipoRegimen = r.TipoRegimen
		out.Receptor.NumEmpleado = r.NumEmpleado
		out.Receptor.Departamento = r.Departamento
		out.Receptor.Puesto = r.Puesto
		out.Receptor.RiesgoPuesto = r.RiesgoPuesto
		out.Receptor.PeriodicidadPago = r.PeriodicidadPago
		out.Receptor.Banco = r.Banco
		out.Receptor.CuentaBancaria = r.CuentaBancaria
		out.Receptor.SalarioBaseCotApor = r.SalarioBaseCotApor
		out.Receptor.SalarioDiarioIntegrado = r.SalarioDiarioIntegrado
		out.Receptor.ClaveEntFed = r.ClaveEntFed
	}
	if n.Percepciones != nil {
		p := n.Percepciones
		out.Percepciones.TotalSueldos = p.TotalSueldos
		out.Percepciones.TotalSeparacionIndemnizacion = p.TotalSeparacionIndemnizacion
		out.Percepciones.TotalJubilacionPensionRetiro = p.TotalJubilacionPensionRetiro
		out.Percepciones.TotalGravado = p.TotalGravado
		out.Percepciones.TotalExento = p.TotalExento
	}
	if n.Deducciones != nil {
		d := n.Deducciones
		out.Deducciones.TotalOtrasDeducciones = d.TotalOtrasDeducciones
		out.Deducciones.TotalImpuestosRetenidos = d.TotalImpuestosRetenidos
		// TipoDeduccion="007" = Ajuste en Subsidio para el Empleo (Diferencia en Subsidio)
		for _, ded := range d.Deduccion {
			if strings.TrimSpace(ded.TipoDeduccion) == "007" {
				out.AjusteISRRetenido += parseFloatStr(ded.Importe)
			}
		}
	}
	if n.OtrosPagos != nil {
		for _, op := range n.OtrosPagos.OtroPago {
			if strings.TrimSpace(op.TipoOtroPago) == "002" && op.SubsidioAlEmpleo != nil {
				out.SubsidioCausado += parseFloatStr(op.SubsidioAlEmpleo.SubsidioCausado)
			}
		}
	}

	return out, nil
}
