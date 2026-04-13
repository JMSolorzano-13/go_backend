package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// ProcessXML handles SAT_CFDIS_DOWNLOADED messages.
//
// Pipeline step 5: download package ZIPs from blob → extract XML files → parse
// CFDI data → update existing cfdi rows (from_xml=true, populate XML-only fields)
// → update query state=PROCESSED.
//
// Mirrors Python XMLProcessor.process.
type ProcessXML struct {
	Deps
}

func (h *ProcessXML) Handle(ctx context.Context, raw json.RawMessage) error {
	var msg ProcessQueryMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("unmarshal ProcessQueryMsg: %w", err)
	}

	conn, err := h.DB.TenantConn(ctx, msg.CompanyIdentifier, false)
	if err != nil {
		return fmt.Errorf("tenant conn: %w", err)
	}
	defer conn.Close()

	// Fallback: if packages not in the message, load from sat_query row.
	if len(msg.Packages) == 0 {
		var row tenant.SATQuery
		if scanErr := conn.NewSelect().
			Model(&row).
			Column("packages").
			Where("identifier = ?", msg.QueryIdentifier).
			Scan(ctx); scanErr == nil && len(row.Packages) > 0 {
			var pkgs []string
			if jErr := json.Unmarshal(row.Packages, &pkgs); jErr == nil {
				msg.Packages = pkgs
			}
		}
	}

	logger := slog.With(
		"handler", "ProcessXML",
		"company", msg.CompanyIdentifier,
		"query", msg.QueryIdentifier,
		"packages", len(msg.Packages),
	)

	companyRFC, err := h.getCompanyRFC(ctx, msg.CompanyIdentifier)
	if err != nil {
		return fmt.Errorf("get company RFC: %w", err)
	}

	totalProcessed := 0
	for _, pkgID := range msg.Packages {
		n, processErr := h.processPackageXMLs(ctx, conn, pkgID, msg.CompanyIdentifier, companyRFC, logger)
		if processErr != nil {
			logger.Error("failed to process XML package", "package", pkgID, "error", processErr)
			continue
		}
		totalProcessed += n
	}

	// Mark query as PROCESSED.
	now := time.Now().UTC()
	if _, err := conn.NewUpdate().
		Model((*tenant.SATQuery)(nil)).
		Set("state = ?", tenant.QueryStateProcessed).
		Set("updated_at = ?", now).
		Where("identifier = ?", msg.QueryIdentifier).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark query PROCESSED: %w", err)
	}

	logger.Info("XML processing complete", "total_xmls_processed", totalProcessed)
	return nil
}

// processPackageXMLs downloads a ZIP, extracts .xml files, parses them,
// and upserts CFDI data. Returns the count of XMLs processed.
func (h *ProcessXML) processPackageXMLs(ctx context.Context, conn bun.Conn, packageID, companyID, companyRFC string, logger *slog.Logger) (int, error) {
	bucket := h.Cfg.S3Attachments
	blobKey := fmt.Sprintf("attachments/Zips/%s.zip", packageID)

	zipData, err := h.Storage.Download(ctx, bucket, blobKey)
	if err != nil {
		return 0, fmt.Errorf("download package ZIP: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return 0, fmt.Errorf("open ZIP: %w", err)
	}

	now := time.Now().UTC()
	processed := 0
	seen := make(map[string]bool)

	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".xml") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			logger.Error("failed to open XML in ZIP", "file", f.Name, "error", err)
			continue
		}

		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			rc.Close()
			logger.Error("failed to read XML", "file", f.Name, "error", err)
			continue
		}
		rc.Close()

		// Strip UTF-8 BOM (U+FEFF = 0xEF 0xBB 0xBF) before both parsing and storing.
		// SAT occasionally prepends a BOM to XML files.
		// Mirrors: xml_content = xml_content.replace("\ufeff", "") in Python cfdi_parser.py.
		xmlContent := strings.TrimPrefix(buf.String(), "\uFEFF")

		cfdiData, err := parseCFDIXML(xmlContent, companyRFC)
		if err != nil {
			logger.Debug("skipping unparseable XML", "file", f.Name, "error", err)
			continue
		}

		if seen[cfdiData.UUID] {
			logger.Debug("duplicate XML in batch", "uuid", cfdiData.UUID)
			continue
		}
		seen[cfdiData.UUID] = true

		xmlContent = stripXMLDecl(xmlContent)

		if cfdiData.TipoDeComprobante == "P" {
			if applyPagosComplementToCFDIData(cfdiData, xmlContent) {
				recomputeCFDIDataMXN(cfdiData)
			}
		}

		if err := h.upsertFromXML(ctx, conn, cfdiData, companyID, xmlContent, now); err != nil {
			logger.Error("upsert from XML failed", "uuid", cfdiData.UUID, "error", err)
			continue
		}

		// Populate related tables — errors are logged but never abort CFDI processing.
		h.insertCfdiRelaciones(ctx, conn, cfdiData, companyID, now, logger)
		h.insertPayments(ctx, conn, cfdiData, companyID, xmlContent, now, logger)
		h.insertNomina(ctx, conn, cfdiData, companyID, xmlContent, now, logger)

		processed++
	}

	return processed, nil
}

// cfdiXMLData holds all data parsed from a CFDI XML file.
type cfdiXMLData struct {
	UUID              string
	Version           string
	Fecha             time.Time
	Total             float64
	SubTotal          float64
	TipoDeComprobante string
	FormaPago         string
	MetodoPago        string
	Moneda            string
	TipoCambio        float64
	LugarExpedicion   string
	Folio             string
	Serie             string
	Descuento         float64
	Exportacion       string
	Sello             string
	NoCertificado     string
	Certificado       string
	CondicionesDePago string

	// Emisor
	RfcEmisor           string
	NombreEmisor        string
	RegimenFiscalEmisor string

	// Receptor
	RfcReceptor             string
	NombreReceptor          string
	UsoCFDIReceptor         string
	DomicilioFiscalReceptor string
	RegimenFiscalReceptor   string

	// TimbreFiscalDigital
	RfcPac                string
	FechaCertificacionSat time.Time
	NoCertificadoSAT      string
	SelloSAT              string

	// Taxes
	TrasladosIVA    float64
	RetencionesIVA  float64
	TrasladosISR    float64
	RetencionesISR  float64
	TrasladosIEPS   float64
	RetencionesIEPS float64
	Neto            float64

	// IVA bases
	BaseIVA16       float64
	BaseIVA8        float64
	BaseIVA0        float64
	BaseIVAExento   float64
	IVATrasladado16 float64
	IVATrasladado8  float64

	// MXN currency-converted fields (value × TipoCambio, mirrors Python compute_mxn_fields)
	TotalMXN           float64
	SubTotalMXN        float64
	NetoMXN            float64
	DescuentoMXN       float64
	TrasladosIVAMXN    float64
	TrasladosIEPSMXN   float64
	TrasladosISRMXN    float64
	RetencionesIVAMXN  float64
	RetencionesIEPSMXN float64
	RetencionesISRMXN  float64

	// Boolean classification flags (mirrors Python parsers.add_validations)
	IsIPUE                bool
	IsIPPD                bool
	IsEPPD                bool
	IsENoCfdiRelacionados bool

	// JSON serializations (xmltodict-compatible for Python interop)
	ConceptosJSON string
	ImpuestosJSON string

	IsIssued         bool
	CfdiRelacionados string

	// Pagos complement (tipo P): FechaFiltro/PaymentDate use first Pago/@FechaPago (Python _set_fecha_filtro).
	PagosFechaFiltro       time.Time
	PagosComplementApplied bool
}

// parseCFDIXML extracts all CFDI fields from an XML string.
// The BOM must already be stripped from xmlContent before calling this function.
func parseCFDIXML(xmlContent, companyRFC string) (*cfdiXMLData, error) {
	// All types are local to avoid polluting the package namespace.
	type Traslado struct {
		Impuesto   string `xml:"Impuesto,attr"`
		TipoFactor string `xml:"TipoFactor,attr"`
		TasaOCuota string `xml:"TasaOCuota,attr"`
		Importe    string `xml:"Importe,attr"`
		Base       string `xml:"Base,attr"`
	}
	type Retencion struct {
		Impuesto string `xml:"Impuesto,attr"`
		Importe  string `xml:"Importe,attr"`
	}
	type Traslados struct {
		Traslado []Traslado `xml:"Traslado"`
	}
	type Retenciones struct {
		Retencion []Retencion `xml:"Retencion"`
	}
	type Impuestos struct {
		TotalImpuestosTrasladados string       `xml:"TotalImpuestosTrasladados,attr"`
		TotalImpuestosRetenidos   string       `xml:"TotalImpuestosRetenidos,attr"`
		Traslados                 *Traslados   `xml:"Traslados"`
		Retenciones               *Retenciones `xml:"Retenciones"`
	}
	type Emisor struct {
		Rfc           string `xml:"Rfc,attr"`
		Nombre        string `xml:"Nombre,attr"`
		RegimenFiscal string `xml:"RegimenFiscal,attr"`
	}
	type Receptor struct {
		Rfc                     string `xml:"Rfc,attr"`
		Nombre                  string `xml:"Nombre,attr"`
		UsoCFDI                 string `xml:"UsoCFDI,attr"`
		DomicilioFiscalReceptor string `xml:"DomicilioFiscalReceptor,attr"`
		RegimenFiscalReceptor   string `xml:"RegimenFiscalReceptor,attr"`
	}
	type CfdiRelacionado struct {
		UUID string `xml:"UUID,attr"`
	}
	type CfdiRelacionados struct {
		TipoRelacion    string            `xml:"TipoRelacion,attr"`
		CfdiRelacionado []CfdiRelacionado `xml:"CfdiRelacionado"`
	}
	type TimbreFiscalDigital struct {
		UUID             string `xml:"UUID,attr"`
		FechaTimbrado    string `xml:"FechaTimbrado,attr"`
		RfcProvCertif    string `xml:"RfcProvCertif,attr"`
		NoCertificadoSAT string `xml:"NoCertificadoSAT,attr"`
		SelloSAT         string `xml:"SelloSAT,attr"`
	}
	type Complemento struct {
		TimbreFiscal TimbreFiscalDigital `xml:"TimbreFiscalDigital"`
	}

	// Concepto types for Conceptos JSON serialization.
	type CptoTraslado struct {
		Base       string `xml:"Base,attr"`
		Impuesto   string `xml:"Impuesto,attr"`
		TipoFactor string `xml:"TipoFactor,attr"`
		TasaOCuota string `xml:"TasaOCuota,attr"`
		Importe    string `xml:"Importe,attr"`
	}
	type CptoRetencion struct {
		Base       string `xml:"Base,attr"`
		Impuesto   string `xml:"Impuesto,attr"`
		TipoFactor string `xml:"TipoFactor,attr"`
		TasaOCuota string `xml:"TasaOCuota,attr"`
		Importe    string `xml:"Importe,attr"`
	}
	type CptoTraslados struct {
		Traslado []CptoTraslado `xml:"Traslado"`
	}
	type CptoRetenciones struct {
		Retencion []CptoRetencion `xml:"Retencion"`
	}
	type CptoImpuestos struct {
		Traslados   *CptoTraslados   `xml:"Traslados"`
		Retenciones *CptoRetenciones `xml:"Retenciones"`
	}
	type Concepto struct {
		ClaveProdServ    string         `xml:"ClaveProdServ,attr"`
		NoIdentificacion string         `xml:"NoIdentificacion,attr"`
		Cantidad         string         `xml:"Cantidad,attr"`
		ClaveUnidad      string         `xml:"ClaveUnidad,attr"`
		Unidad           string         `xml:"Unidad,attr"`
		Descripcion      string         `xml:"Descripcion,attr"`
		ValorUnitario    string         `xml:"ValorUnitario,attr"`
		Importe          string         `xml:"Importe,attr"`
		Descuento        string         `xml:"Descuento,attr"`
		ObjetoImp        string         `xml:"ObjetoImp,attr"`
		Impuestos        *CptoImpuestos `xml:"Impuestos"`
	}
	type Conceptos struct {
		Concepto []Concepto `xml:"Concepto"`
	}

	type Comprobante struct {
		XMLName           xml.Name           `xml:"Comprobante"`
		Version           string             `xml:"Version,attr"`
		Fecha             string             `xml:"Fecha,attr"`
		Total             string             `xml:"Total,attr"`
		SubTotal          string             `xml:"SubTotal,attr"`
		TipoDeComprobante string             `xml:"TipoDeComprobante,attr"`
		FormaPago         string             `xml:"FormaPago,attr"`
		MetodoPago        string             `xml:"MetodoPago,attr"`
		Moneda            string             `xml:"Moneda,attr"`
		TipoCambio        string             `xml:"TipoCambio,attr"`
		LugarExpedicion   string             `xml:"LugarExpedicion,attr"`
		Folio             string             `xml:"Folio,attr"`
		Serie             string             `xml:"Serie,attr"`
		Descuento         string             `xml:"Descuento,attr"`
		Exportacion       string             `xml:"Exportacion,attr"`
		Sello             string             `xml:"Sello,attr"`
		NoCertificado     string             `xml:"NoCertificado,attr"`
		Certificado       string             `xml:"Certificado,attr"`
		CondicionesDePago string             `xml:"CondicionesDePago,attr"`
		Emisor            Emisor             `xml:"Emisor"`
		Receptor          Receptor           `xml:"Receptor"`
		Impuestos         *Impuestos         `xml:"Impuestos"`
		Conceptos         *Conceptos         `xml:"Conceptos"`
		CfdiRelacionados  []CfdiRelacionados `xml:"CfdiRelacionados"`
		Complemento       Complemento        `xml:"Complemento"`
	}

	var comp Comprobante
	if err := xml.Unmarshal([]byte(xmlContent), &comp); err != nil {
		return nil, fmt.Errorf("unmarshal CFDI XML: %w", err)
	}

	if comp.Version != "3.3" && comp.Version != "4.0" {
		return nil, fmt.Errorf("unsupported CFDI version: %s", comp.Version)
	}

	tfd := comp.Complemento.TimbreFiscal
	uuid := strings.ToLower(strings.TrimSpace(tfd.UUID))
	if uuid == "" {
		return nil, fmt.Errorf("no TimbreFiscalDigital UUID found")
	}

	data := &cfdiXMLData{
		UUID:                    uuid,
		Version:                 comp.Version,
		Fecha:                   parseDatetime(comp.Fecha),
		Total:                   parseFloatStr(comp.Total),
		SubTotal:                parseFloatStr(comp.SubTotal),
		TipoDeComprobante:       comp.TipoDeComprobante,
		FormaPago:               comp.FormaPago,
		MetodoPago:              comp.MetodoPago,
		Moneda:                  comp.Moneda,
		TipoCambio:              parseFloatStr(comp.TipoCambio),
		LugarExpedicion:         comp.LugarExpedicion,
		Folio:                   comp.Folio,
		Serie:                   comp.Serie,
		Descuento:               parseFloatStr(comp.Descuento),
		Exportacion:             comp.Exportacion,
		Sello:                   comp.Sello,
		NoCertificado:           comp.NoCertificado,
		Certificado:             comp.Certificado,
		CondicionesDePago:       comp.CondicionesDePago,
		RfcEmisor:               comp.Emisor.Rfc,
		NombreEmisor:            comp.Emisor.Nombre,
		RegimenFiscalEmisor:     comp.Emisor.RegimenFiscal,
		RfcReceptor:             comp.Receptor.Rfc,
		NombreReceptor:          comp.Receptor.Nombre,
		UsoCFDIReceptor:         comp.Receptor.UsoCFDI,
		DomicilioFiscalReceptor: comp.Receptor.DomicilioFiscalReceptor,
		RegimenFiscalReceptor:   comp.Receptor.RegimenFiscalReceptor,
		IsIssued:                comp.Emisor.Rfc == companyRFC,
		// TimbreFiscalDigital fields
		RfcPac:                strings.TrimSpace(tfd.RfcProvCertif),
		FechaCertificacionSat: parseDatetime(tfd.FechaTimbrado),
		NoCertificadoSAT:      strings.TrimSpace(tfd.NoCertificadoSAT),
		SelloSAT:              strings.TrimSpace(tfd.SelloSAT),
	}

	data.Neto = data.SubTotal - data.Descuento

	// Parse tax details and build Impuestos JSON map simultaneously.
	impuestosMap := make(map[string]interface{})
	if comp.Impuestos != nil {
		if comp.Impuestos.TotalImpuestosTrasladados != "" {
			impuestosMap["@TotalImpuestosTrasladados"] = comp.Impuestos.TotalImpuestosTrasladados
		}
		if comp.Impuestos.TotalImpuestosRetenidos != "" {
			impuestosMap["@TotalImpuestosRetenidos"] = comp.Impuestos.TotalImpuestosRetenidos
		}

		if comp.Impuestos.Traslados != nil {
			trasladoItems := make([]interface{}, 0, len(comp.Impuestos.Traslados.Traslado))
			for _, t := range comp.Impuestos.Traslados.Traslado {
				importe := parseFloatStr(t.Importe)
				base := parseFloatStr(t.Base)
				tasa := parseFloatStr(t.TasaOCuota)

				switch t.Impuesto {
				case "002": // IVA
					data.TrasladosIVA += importe
					switch {
					case tasa >= 0.15 && tasa <= 0.17: // 16%
						data.BaseIVA16 += base
						data.IVATrasladado16 += importe
					case tasa >= 0.07 && tasa <= 0.09: // 8%
						data.BaseIVA8 += base
						data.IVATrasladado8 += importe
					case tasa == 0:
						data.BaseIVA0 += base
					}
					if t.TipoFactor == "Exento" {
						data.BaseIVAExento += base
					}
				case "003": // IEPS
					data.TrasladosIEPS += importe
				case "001": // ISR
					data.TrasladosISR += importe
				}

				trasladoItems = append(trasladoItems, map[string]interface{}{
					"@Base":       t.Base,
					"@Impuesto":   t.Impuesto,
					"@TipoFactor": t.TipoFactor,
					"@TasaOCuota": t.TasaOCuota,
					"@Importe":    t.Importe,
				})
			}
			if len(trasladoItems) > 0 {
				impuestosMap["Traslados"] = map[string]interface{}{
					"Traslado": xmltodictItem(trasladoItems),
				}
			}
		}

		if comp.Impuestos.Retenciones != nil {
			retencionItems := make([]interface{}, 0, len(comp.Impuestos.Retenciones.Retencion))
			for _, r := range comp.Impuestos.Retenciones.Retencion {
				importe := parseFloatStr(r.Importe)
				switch r.Impuesto {
				case "002":
					data.RetencionesIVA += importe
				case "003":
					data.RetencionesIEPS += importe
				case "001":
					data.RetencionesISR += importe
				}
				retencionItems = append(retencionItems, map[string]interface{}{
					"@Impuesto": r.Impuesto,
					"@Importe":  r.Importe,
				})
			}
			if len(retencionItems) > 0 {
				impuestosMap["Retenciones"] = map[string]interface{}{
					"Retencion": xmltodictItem(retencionItems),
				}
			}
		}
	}
	if b, err := json.Marshal(impuestosMap); err == nil {
		data.ImpuestosJSON = string(b)
	} else {
		data.ImpuestosJSON = "{}"
	}

	// Build Conceptos JSON map (xmltodict-compatible).
	if comp.Conceptos != nil && len(comp.Conceptos.Concepto) > 0 {
		conceptoItems := make([]interface{}, 0, len(comp.Conceptos.Concepto))
		for _, c := range comp.Conceptos.Concepto {
			cm := map[string]interface{}{
				"@ClaveProdServ": c.ClaveProdServ,
				"@Cantidad":      c.Cantidad,
				"@ClaveUnidad":   c.ClaveUnidad,
				"@Descripcion":   c.Descripcion,
				"@ValorUnitario": c.ValorUnitario,
				"@Importe":       c.Importe,
			}
			if c.NoIdentificacion != "" {
				cm["@NoIdentificacion"] = c.NoIdentificacion
			}
			if c.Unidad != "" {
				cm["@Unidad"] = c.Unidad
			}
			if c.ObjetoImp != "" {
				cm["@ObjetoImp"] = c.ObjetoImp
			}
			if c.Descuento != "" {
				cm["@Descuento"] = c.Descuento
			}

			if c.Impuestos != nil {
				cImpM := make(map[string]interface{})
				if c.Impuestos.Traslados != nil && len(c.Impuestos.Traslados.Traslado) > 0 {
					tItems := make([]interface{}, len(c.Impuestos.Traslados.Traslado))
					for i, t := range c.Impuestos.Traslados.Traslado {
						tItems[i] = map[string]interface{}{
							"@Base":       t.Base,
							"@Impuesto":   t.Impuesto,
							"@TipoFactor": t.TipoFactor,
							"@TasaOCuota": t.TasaOCuota,
							"@Importe":    t.Importe,
						}
					}
					cImpM["Traslados"] = map[string]interface{}{"Traslado": xmltodictItem(tItems)}
				}
				if c.Impuestos.Retenciones != nil && len(c.Impuestos.Retenciones.Retencion) > 0 {
					rItems := make([]interface{}, len(c.Impuestos.Retenciones.Retencion))
					for i, r := range c.Impuestos.Retenciones.Retencion {
						rItems[i] = map[string]interface{}{
							"@Base":       r.Base,
							"@Impuesto":   r.Impuesto,
							"@TipoFactor": r.TipoFactor,
							"@TasaOCuota": r.TasaOCuota,
							"@Importe":    r.Importe,
						}
					}
					cImpM["Retenciones"] = map[string]interface{}{"Retencion": xmltodictItem(rItems)}
				}
				if len(cImpM) > 0 {
					cm["Impuestos"] = cImpM
				}
			}

			conceptoItems = append(conceptoItems, cm)
		}
		if b, err := json.Marshal(map[string]interface{}{
			"Concepto": xmltodictItem(conceptoItems),
		}); err == nil {
			data.ConceptosJSON = string(b)
		}
	}

	// Serialize CfdiRelacionados to JSON.
	if len(comp.CfdiRelacionados) > 0 {
		relJSON, _ := json.Marshal(comp.CfdiRelacionados)
		data.CfdiRelacionados = string(relJSON)
	}

	recomputeCFDIDataMXN(data)

	// Compute boolean classification flags (mirrors Python parsers.add_validations).
	// TipoDeComprobante_I_MetodoPago_PUE: Ingreso + PUE payment method + 99 FormaPago.
	data.IsIPUE = comp.TipoDeComprobante == "I" && comp.MetodoPago == "PUE" && comp.FormaPago == "99"
	data.IsIPPD = false // Python hardcodes false
	data.IsEPPD = false // Python hardcodes false
	data.IsENoCfdiRelacionados = comp.TipoDeComprobante == "E" && len(comp.CfdiRelacionados) == 0

	return data, nil
}

// recomputeCFDIDataMXN mirrors Python compute_mxn_fields (value × TipoCambio, default 1).
func recomputeCFDIDataMXN(data *cfdiXMLData) {
	if data == nil {
		return
	}
	tc := data.TipoCambio
	if tc <= 0 {
		tc = 1
	}
	data.TotalMXN = roundFloat6(data.Total * tc)
	data.SubTotalMXN = roundFloat6(data.SubTotal * tc)
	data.NetoMXN = roundFloat6(data.Neto * tc)
	data.DescuentoMXN = roundFloat6(data.Descuento * tc)
	data.TrasladosIVAMXN = roundFloat6(data.TrasladosIVA * tc)
	data.TrasladosIEPSMXN = roundFloat6(data.TrasladosIEPS * tc)
	data.TrasladosISRMXN = roundFloat6(data.TrasladosISR * tc)
	data.RetencionesIVAMXN = roundFloat6(data.RetencionesIVA * tc)
	data.RetencionesIEPSMXN = roundFloat6(data.RetencionesIEPS * tc)
	data.RetencionesISRMXN = roundFloat6(data.RetencionesISR * tc)
}

// cfdiFechaFiltroForXML returns FechaFiltro/PaymentDate for persistence (Pagos use complement FechaPago).
func cfdiFechaFiltroForXML(data *cfdiXMLData) time.Time {
	if data != nil && data.PagosComplementApplied && !data.PagosFechaFiltro.IsZero() {
		return data.PagosFechaFiltro
	}
	if data != nil {
		return data.Fecha
	}
	return time.Time{}
}

// upsertFromXML updates an existing CFDI row with data parsed from the XML,
// or inserts a new row if it doesn't exist yet.
func (h *ProcessXML) upsertFromXML(ctx context.Context, conn bun.Conn, data *cfdiXMLData, companyID, xmlContent string, now time.Time) error {
	q := conn.NewUpdate().
		Model((*tenant.CFDI)(nil)).
		Set("from_xml = true").
		Set(`"Version" = ?`, data.Version).
		Set(`"SubTotal" = ?`, data.SubTotal).
		Set(`"FormaPago" = ?`, nilIfEmpty(data.FormaPago)).
		Set(`"MetodoPago" = ?`, nilIfEmpty(data.MetodoPago)).
		Set(`"Moneda" = ?`, nilIfEmpty(data.Moneda)).
		Set(`"TipoCambio" = ?`, nilIfZero(data.TipoCambio)).
		Set(`"LugarExpedicion" = ?`, nilIfEmpty(data.LugarExpedicion)).
		Set(`"Folio" = ?`, nilIfEmpty(data.Folio)).
		Set(`"Serie" = ?`, nilIfEmpty(data.Serie)).
		Set(`"Descuento" = ?`, nilIfZero(data.Descuento)).
		Set(`"Exportacion" = ?`, nilIfEmpty(data.Exportacion)).
		Set(`"Sello" = ?`, nilIfEmpty(data.Sello)).
		Set(`"NoCertificado" = ?`, nilIfEmpty(data.NoCertificado)).
		Set(`"Certificado" = ?`, nilIfEmpty(data.Certificado)).
		Set(`"CondicionesDePago" = ?`, nilIfEmpty(data.CondicionesDePago)).
		Set(`"RegimenFiscalEmisor" = ?`, nilIfEmpty(data.RegimenFiscalEmisor)).
		Set(`"UsoCFDIReceptor" = ?`, nilIfEmpty(data.UsoCFDIReceptor)).
		Set(`"DomicilioFiscalReceptor" = ?`, nilIfEmpty(data.DomicilioFiscalReceptor)).
		Set(`"RegimenFiscalReceptor" = ?`, nilIfEmpty(data.RegimenFiscalReceptor)).
		Set(`"NoCertificadoSAT" = ?`, nilIfEmpty(data.NoCertificadoSAT)).
		Set(`"SelloSAT" = ?`, nilIfEmpty(data.SelloSAT)).
		Set(`"Conceptos" = ?`, nilIfEmpty(data.ConceptosJSON)).
		Set(`"Impuestos" = ?`, nilIfEmpty(data.ImpuestosJSON)).
		Set(`"Neto" = ?`, data.Neto).
		Set(`"TrasladosIVA" = ?`, nilIfZero(data.TrasladosIVA)).
		Set(`"TrasladosIEPS" = ?`, nilIfZero(data.TrasladosIEPS)).
		Set(`"TrasladosISR" = ?`, nilIfZero(data.TrasladosISR)).
		Set(`"RetencionesIVA" = ?`, nilIfZero(data.RetencionesIVA)).
		Set(`"RetencionesIEPS" = ?`, nilIfZero(data.RetencionesIEPS)).
		Set(`"RetencionesISR" = ?`, nilIfZero(data.RetencionesISR)).
		Set(`"BaseIVA16" = ?`, nilIfZero(data.BaseIVA16)).
		Set(`"BaseIVA8" = ?`, nilIfZero(data.BaseIVA8)).
		Set(`"BaseIVA0" = ?`, nilIfZero(data.BaseIVA0)).
		Set(`"BaseIVAExento" = ?`, nilIfZero(data.BaseIVAExento)).
		Set(`"IVATrasladado16" = ?`, nilIfZero(data.IVATrasladado16)).
		Set(`"IVATrasladado8" = ?`, nilIfZero(data.IVATrasladado8)).
		Set(`"TotalMXN" = ?`, nilIfZero(data.TotalMXN)).
		Set(`"SubTotalMXN" = ?`, nilIfZero(data.SubTotalMXN)).
		Set(`"NetoMXN" = ?`, nilIfZero(data.NetoMXN)).
		Set(`"DescuentoMXN" = ?`, nilIfZero(data.DescuentoMXN)).
		Set(`"TrasladosIVAMXN" = ?`, nilIfZero(data.TrasladosIVAMXN)).
		Set(`"TrasladosIEPSMXN" = ?`, nilIfZero(data.TrasladosIEPSMXN)).
		Set(`"TrasladosISRMXN" = ?`, nilIfZero(data.TrasladosISRMXN)).
		Set(`"RetencionesIVAMXN" = ?`, nilIfZero(data.RetencionesIVAMXN)).
		Set(`"RetencionesIEPSMXN" = ?`, nilIfZero(data.RetencionesIEPSMXN)).
		Set(`"RetencionesISRMXN" = ?`, nilIfZero(data.RetencionesISRMXN)).
		Set(`"TipoDeComprobante_I_MetodoPago_PUE" = ?`, data.IsIPUE).
		Set(`"TipoDeComprobante_I_MetodoPago_PPD" = ?`, data.IsIPPD).
		Set(`"TipoDeComprobante_E_MetodoPago_PPD" = ?`, data.IsEPPD).
		Set(`"TipoDeComprobante_E_CfdiRelacionados_None" = ?`, data.IsENoCfdiRelacionados).
		Set(`"CfdiRelacionados" = ?`, nilIfEmpty(data.CfdiRelacionados)).
		Set("xml_content = ?", xmlContent).
		Set("updated_at = ?", now).
		Where("company_identifier = ?", companyID).
		Where(`"UUID" = ?`, data.UUID)

	// Only overwrite FechaCertificacionSat / RfcPac from TFD if successfully parsed.
	// Metadata processing sets these first; this ensures consistency when XML runs alone.
	if !data.FechaCertificacionSat.IsZero() {
		q = q.Set(`"FechaCertificacionSat" = ?`, data.FechaCertificacionSat)
	}
	if data.RfcPac != "" {
		q = q.Set(`"RfcPac" = ?`, data.RfcPac)
	}

	q = q.Set(`"Total" = ?`, data.Total)
	if data.PagosComplementApplied {
		ff := cfdiFechaFiltroForXML(data)
		q = q.Set(`"FechaFiltro" = ?`, ff).Set(`"PaymentDate" = ?`, ff)
	}

	res, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("update CFDI from XML: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		// Row doesn't exist yet — insert it (rare: XML arrives before metadata).
		fechaCert := data.FechaCertificacionSat
		if fechaCert.IsZero() {
			fechaCert = data.Fecha
		}
		ffIns := cfdiFechaFiltroForXML(data)
		cfdi := &tenant.CFDI{
			CompanyIdentifier:                      companyID,
			IsIssued:                               data.IsIssued,
			UUID:                                   data.UUID,
			Fecha:                                  data.Fecha,
			Total:                                  data.Total,
			TipoDeComprobante:                      data.TipoDeComprobante,
			RfcEmisor:                              data.RfcEmisor,
			NombreEmisor:                           &data.NombreEmisor,
			RfcReceptor:                            data.RfcReceptor,
			NombreReceptor:                         &data.NombreReceptor,
			RfcPac:                                 nilIfEmpty(data.RfcPac),
			FechaCertificacionSat:                  fechaCert,
			Estatus:                                true,
			FechaFiltro:                            ffIns,
			PaymentDate:                            ffIns,
			Active:                                 true,
			FromXML:                                true,
			NoCertificadoSAT:                       nilIfEmpty(data.NoCertificadoSAT),
			SelloSAT:                               nilIfEmpty(data.SelloSAT),
			CondicionesDePago:                      nilIfEmpty(data.CondicionesDePago),
			Conceptos:                              nilIfEmpty(data.ConceptosJSON),
			Impuestos:                              nilIfEmpty(data.ImpuestosJSON),
			TotalMXN:                               nilIfZero(data.TotalMXN),
			SubTotalMXN:                            nilIfZero(data.SubTotalMXN),
			NetoMXN:                                nilIfZero(data.NetoMXN),
			DescuentoMXN:                           nilIfZero(data.DescuentoMXN),
			TrasladosIVAMXN:                        nilIfZero(data.TrasladosIVAMXN),
			TrasladosIEPSMXN:                       nilIfZero(data.TrasladosIEPSMXN),
			TrasladosISRMXN:                        nilIfZero(data.TrasladosISRMXN),
			RetencionesIVAMXN:                      nilIfZero(data.RetencionesIVAMXN),
			RetencionesIEPSMXN:                     nilIfZero(data.RetencionesIEPSMXN),
			RetencionesISRMXN:                      nilIfZero(data.RetencionesISRMXN),
			TipoDeComprobanteIMetodoPagoPUE:        data.IsIPUE,
			TipoDeComprobanteIMetodoPagoPPD:        data.IsIPPD,
			TipoDeComprobanteEMetodoPagoPPD:        data.IsEPPD,
			TipoDeComprobanteECfdiRelacionadosNone: data.IsENoCfdiRelacionados,
			CreatedAt:                              now,
			UpdatedAt:                              now,
			XMLContent:                             &xmlContent,
		}

		if _, err := conn.NewInsert().
			Model(cfdi).
			On(`CONFLICT (company_identifier, is_issued, "UUID") DO NOTHING`).
			Exec(ctx); err != nil {
			return fmt.Errorf("insert CFDI from XML: %w", err)
		}
	}

	return nil
}

// getCompanyRFC looks up the company RFC from the control database.
func (h *ProcessXML) getCompanyRFC(ctx context.Context, companyIdentifier string) (string, error) {
	var rfc string
	err := h.DB.Primary.NewSelect().
		TableExpr("company").
		Column("rfc").
		Where("identifier = ?", companyIdentifier).
		Scan(ctx, &rfc)
	if err != nil {
		return "", err
	}
	return rfc, nil
}

var reXMLDecl = regexp.MustCompile(`<\?xml[^?]*\?>`)

// stripXMLDecl removes <?xml ...?> so PostgreSQL's xml type doesn't reject
// encoding declarations that mismatch the server encoding.
// BOM is stripped earlier in processPackageXMLs before this is called.
func stripXMLDecl(s string) string {
	return strings.TrimSpace(reXMLDecl.ReplaceAllString(s, ""))
}

// xmltodictItem returns the single element as-is when len==1, or the full slice
// when len>1. This mirrors xmltodict's behavior: single child elements become a
// dict (object), multiple become a list — preserving Python interoperability.
func xmltodictItem(items []interface{}) interface{} {
	if len(items) == 1 {
		return items[0]
	}
	return items
}

// roundFloat6 rounds to 6 decimal places, matching Python's round(value, 6).
func roundFloat6(f float64) float64 {
	return math.Round(f*1e6) / 1e6
}

// parseFloatStr parses a string to float64, returning 0 on error.
func parseFloatStr(s string) float64 {
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nilIfZero(f float64) *float64 {
	if f == 0 {
		return nil
	}
	return &f
}
