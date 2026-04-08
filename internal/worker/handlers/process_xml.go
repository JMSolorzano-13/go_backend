package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
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

	logger := slog.With(
		"handler", "ProcessXML",
		"company", msg.CompanyIdentifier,
		"query", msg.QueryIdentifier,
		"packages", len(msg.Packages),
	)

	conn, err := h.DB.TenantConn(ctx, msg.CompanyIdentifier, false)
	if err != nil {
		return fmt.Errorf("tenant conn: %w", err)
	}
	defer conn.Close()

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

		xmlContent := buf.String()

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

		// Update existing CFDI row with XML data.
		if err := h.upsertFromXML(ctx, conn, cfdiData, companyID, xmlContent, now); err != nil {
			logger.Error("upsert from XML failed", "uuid", cfdiData.UUID, "error", err)
			continue
		}

		processed++
	}

	return processed, nil
}

// cfdiXMLData holds the parsed data from a CFDI XML.
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

	// Emisor
	RfcEmisor            string
	NombreEmisor         string
	RegimenFiscalEmisor  string

	// Receptor
	RfcReceptor               string
	NombreReceptor            string
	UsoCFDIReceptor           string
	DomicilioFiscalReceptor   string
	RegimenFiscalReceptor     string

	// Taxes
	TrasladosIVA  float64
	RetencionesIVA float64
	TrasladosISR  float64
	RetencionesISR float64
	TrasladosIEPS float64
	RetencionesIEPS float64
	Neto           float64

	// IVA bases
	BaseIVA16    float64
	BaseIVA8     float64
	BaseIVA0     float64
	BaseIVAExento float64
	IVATrasladado16 float64
	IVATrasladado8  float64

	IsIssued    bool
	CfdiRelacionados string
}

// parseCFDIXML extracts key fields from a CFDI XML string.
func parseCFDIXML(xmlContent, companyRFC string) (*cfdiXMLData, error) {
	// Minimal CFDI XML structure for parsing.
	type Traslado struct {
		Impuesto string  `xml:"Impuesto,attr"`
		TasaOCuota string `xml:"TasaOCuota,attr"`
		Importe  string  `xml:"Importe,attr"`
		Base     string  `xml:"Base,attr"`
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
		TotalImpuestosTrasladados string     `xml:"TotalImpuestosTrasladados,attr"`
		TotalImpuestosRetenidos  string     `xml:"TotalImpuestosRetenidos,attr"`
		Traslados                *Traslados  `xml:"Traslados"`
		Retenciones              *Retenciones `xml:"Retenciones"`
	}
	type Emisor struct {
		Rfc           string `xml:"Rfc,attr"`
		Nombre        string `xml:"Nombre,attr"`
		RegimenFiscal string `xml:"RegimenFiscal,attr"`
	}
	type Receptor struct {
		Rfc                    string `xml:"Rfc,attr"`
		Nombre                 string `xml:"Nombre,attr"`
		UsoCFDI                string `xml:"UsoCFDI,attr"`
		DomicilioFiscalReceptor string `xml:"DomicilioFiscalReceptor,attr"`
		RegimenFiscalReceptor  string `xml:"RegimenFiscalReceptor,attr"`
	}
	type CfdiRelacionado struct {
		UUID string `xml:"UUID,attr"`
	}
	type CfdiRelacionados struct {
		TipoRelacion    string            `xml:"TipoRelacion,attr"`
		CfdiRelacionado []CfdiRelacionado `xml:"CfdiRelacionado"`
	}
	type TimbreFiscalDigital struct {
		UUID string `xml:"UUID,attr"`
	}
	type Complemento struct {
		TimbreFiscal TimbreFiscalDigital `xml:"TimbreFiscalDigital"`
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
		Emisor            Emisor             `xml:"Emisor"`
		Receptor          Receptor           `xml:"Receptor"`
		Impuestos         *Impuestos         `xml:"Impuestos"`
		CfdiRelacionados  []CfdiRelacionados `xml:"CfdiRelacionados"`
		Complemento       Complemento        `xml:"Complemento"`
	}

	var comp Comprobante
	if err := xml.Unmarshal([]byte(xmlContent), &comp); err != nil {
		return nil, fmt.Errorf("unmarshal CFDI XML: %w", err)
	}

	// Validate version (only 3.3 and 4.0 supported).
	if comp.Version != "3.3" && comp.Version != "4.0" {
		return nil, fmt.Errorf("unsupported CFDI version: %s", comp.Version)
	}

	uuid := comp.Complemento.TimbreFiscal.UUID
	if uuid == "" {
		return nil, fmt.Errorf("no TimbreFiscalDigital UUID found")
	}
	uuid = strings.ToLower(strings.TrimSpace(uuid))

	data := &cfdiXMLData{
		UUID:                      uuid,
		Version:                   comp.Version,
		Fecha:                     parseDatetime(comp.Fecha),
		Total:                     parseFloatStr(comp.Total),
		SubTotal:                  parseFloatStr(comp.SubTotal),
		TipoDeComprobante:         comp.TipoDeComprobante,
		FormaPago:                 comp.FormaPago,
		MetodoPago:                comp.MetodoPago,
		Moneda:                    comp.Moneda,
		TipoCambio:                parseFloatStr(comp.TipoCambio),
		LugarExpedicion:           comp.LugarExpedicion,
		Folio:                     comp.Folio,
		Serie:                     comp.Serie,
		Descuento:                 parseFloatStr(comp.Descuento),
		Exportacion:               comp.Exportacion,
		Sello:                     comp.Sello,
		NoCertificado:             comp.NoCertificado,
		Certificado:               comp.Certificado,
		RfcEmisor:                 comp.Emisor.Rfc,
		NombreEmisor:              comp.Emisor.Nombre,
		RegimenFiscalEmisor:       comp.Emisor.RegimenFiscal,
		RfcReceptor:               comp.Receptor.Rfc,
		NombreReceptor:            comp.Receptor.Nombre,
		UsoCFDIReceptor:           comp.Receptor.UsoCFDI,
		DomicilioFiscalReceptor:   comp.Receptor.DomicilioFiscalReceptor,
		RegimenFiscalReceptor:     comp.Receptor.RegimenFiscalReceptor,
		IsIssued:                  comp.Emisor.Rfc == companyRFC,
	}

	// Calculate neto = SubTotal - Descuento.
	data.Neto = data.SubTotal - data.Descuento

	// Parse tax details.
	if comp.Impuestos != nil {
		if comp.Impuestos.Traslados != nil {
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
				case "003": // IEPS
					data.TrasladosIEPS += importe
				case "001": // ISR
					data.TrasladosISR += importe
				}
			}
		}
		if comp.Impuestos.Retenciones != nil {
			for _, r := range comp.Impuestos.Retenciones.Retencion {
				importe := parseFloatStr(r.Importe)
				switch r.Impuesto {
				case "002": // IVA
					data.RetencionesIVA += importe
				case "003": // IEPS
					data.RetencionesIEPS += importe
				case "001": // ISR
					data.RetencionesISR += importe
				}
			}
		}
	}

	// Serialize CfdiRelacionados to JSON.
	if len(comp.CfdiRelacionados) > 0 {
		relJSON, _ := json.Marshal(comp.CfdiRelacionados)
		data.CfdiRelacionados = string(relJSON)
	}

	return data, nil
}

// upsertFromXML updates an existing CFDI row with data parsed from the XML,
// or inserts a new row if it doesn't exist yet.
func (h *ProcessXML) upsertFromXML(ctx context.Context, conn bun.Conn, data *cfdiXMLData, companyID, xmlContent string, now time.Time) error {
	// Build the SET clause for all XML-derived fields.
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
		Set(`"RegimenFiscalEmisor" = ?`, nilIfEmpty(data.RegimenFiscalEmisor)).
		Set(`"UsoCFDIReceptor" = ?`, nilIfEmpty(data.UsoCFDIReceptor)).
		Set(`"DomicilioFiscalReceptor" = ?`, nilIfEmpty(data.DomicilioFiscalReceptor)).
		Set(`"RegimenFiscalReceptor" = ?`, nilIfEmpty(data.RegimenFiscalReceptor)).
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
		Set(`"CfdiRelacionados" = ?`, nilIfEmpty(data.CfdiRelacionados)).
		Set("xml_content = ?", xmlContent).
		Set("updated_at = ?", now).
		Where("company_identifier = ?", companyID).
		Where(`"UUID" = ?`, data.UUID)

	res, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("update CFDI from XML: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		// Row doesn't exist yet — insert it (rare case: XML arrives before metadata).
		cfdi := &tenant.CFDI{
			CompanyIdentifier:         companyID,
			IsIssued:                  data.IsIssued,
			UUID:                      data.UUID,
			Fecha:                     data.Fecha,
			Total:                     data.Total,
			TipoDeComprobante:         data.TipoDeComprobante,
			RfcEmisor:                 data.RfcEmisor,
			NombreEmisor:              &data.NombreEmisor,
			RfcReceptor:               data.RfcReceptor,
			NombreReceptor:            &data.NombreReceptor,
			FechaCertificacionSat:     data.Fecha,
			Estatus:                   true,
			FechaFiltro:               data.Fecha,
			PaymentDate:               data.Fecha,
			Active:                    true,
			FromXML:                   true,
			CreatedAt:                 now,
			UpdatedAt:                 now,
			XMLContent:                &xmlContent,
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
