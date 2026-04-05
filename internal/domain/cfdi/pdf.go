package cfdi

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
)


// parseFloatAny converts interface{} (including strings) to float64 for PDF rendering.
func parseFloatAny(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

// cfdiPDFData is the template data model for CFDI PDFs.
type cfdiPDFData struct {
	UUID                    string
	TipoLabel               string
	Estatus                 bool
	FechaCancelacion        string
	Serie                   string
	Folio                   string
	NombreEmisor            string
	RfcEmisor               string
	LugarExpedicion         string
	RegimenFiscalEmisor     string
	NombreReceptor          string
	RfcReceptor             string
	DomicilioFiscalReceptor string
	RegimenFiscalReceptor   string
	Fecha                   string
	NoCertificado           string
	Conceptos               []conceptoPDF
	SubTotal                string
	Descuento               string
	TrasladosIVA            string
	TrasladosISR            string
	TrasladosIEPS           string
	RetencionesIVA          string
	RetencionesISR          string
	RetencionesIEPS         string
	Moneda                  string
	TipoCambioLabel         string
	Total                   string
	FormaPago               string
	MetodoPago              string
	UsoCFDIReceptor         string
	Sello                   string
	SelloSAT                string
	FechaCertificacionSat   string
	NoCertificadoSAT        string
	RfcPac string
}

type conceptoPDF struct {
	ClaveProdServ string
	Cantidad      string
	ClaveUnidad   string
	Descripcion   string
	ValorUnitario string
	Importe       string
}

var tipoLabels = map[string]string{
	"I": "Ingreso",
	"E": "Egreso",
	"T": "Traslado",
	"N": "Nómina",
	"P": "Pago",
}

// htmlToPDF converts an HTML string to PDF bytes using chromedp (Chrome DevTools Protocol).
// chromedp manages Chrome's lifecycle via CDP, avoiding subprocess user-data-dir conflicts.
// The HTML may reference external fonts (e.g., Google Fonts); chromedp's Chrome has network access.
func htmlToPDF(htmlContent string) ([]byte, error) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var pdfBuf []byte
	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frameTree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(frameTree.Frame.ID, htmlContent).Do(ctx)
		}),
		// Wait for all web fonts (Google Fonts) to load before capturing the PDF.
		chromedp.Evaluate(`document.fonts.ready`, nil),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdfBuf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithDisplayHeaderFooter(false).
				WithMarginTop(0.5).
				WithMarginBottom(0.5).
				WithMarginLeft(0.5).
				WithMarginRight(0.5).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("chromedp pdf: %w", err)
	}
	return pdfBuf, nil
}

//go:embed templates/cfdi_pdf.html
var cfdiPDFTemplate string

// renderCFDIHTML renders the HTML template with the given CFDI data.
func renderCFDIHTML(data cfdiPDFData) (string, error) {
	tmpl, err := template.New("cfdi_pdf").Parse(cfdiPDFTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// BuildCFDIPDFZip queries full CFDI data from the domain filters, generates one PDF per CFDI
// using Chrome headless, and returns a ZIP archive.
func BuildCFDIPDFZip(ctx context.Context, db bun.IDB, params crud.SearchParams) ([]byte, error) {
	// Re-query with all fields (empty Fields = all columns) to get full CFDI data for rendering.
	fullParams := params
	fullParams.Fields = nil

	result, err := crud.Search[tenant.CFDI](ctx, db, fullParams, crud.ModelMeta{
		DefaultOrderBy: `"FechaFiltro" DESC`,
		FuzzyFields:    []string{"NombreEmisor", "NombreReceptor", "RfcEmisor", "RfcReceptor", "UUID"},
		ActiveColumn:   "active",
	})
	if err != nil {
		return nil, fmt.Errorf("search for pdf: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, row := range result.Data {
		data := mapToCFDIPDFData(row)
		html, err := renderCFDIHTML(data)
		if err != nil {
			return nil, fmt.Errorf("render html for %s: %w", data.UUID, err)
		}
		pdfBytes, err := htmlToPDF(html)
		if err != nil {
			return nil, fmt.Errorf("html to pdf for %s: %w", data.UUID, err)
		}
		f, err := zw.Create(data.UUID + ".pdf")
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(pdfBytes); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mapToCFDIPDFData converts a SearchResult row (map) into a cfdiPDFData for template rendering.
func mapToCFDIPDFData(row map[string]interface{}) cfdiPDFData {
	d := cfdiPDFData{}

	d.UUID = strVal(row["UUID"])
	d.Estatus = boolValPDF(row["Estatus"])
	d.Serie = strVal(row["Serie"])
	d.Folio = strVal(row["Folio"])
	d.RfcEmisor = strVal(row["RfcEmisor"])
	d.NombreEmisor = strVal(row["NombreEmisor"])
	d.LugarExpedicion = strVal(row["LugarExpedicion"])
	d.RegimenFiscalEmisor = strVal(row["RegimenFiscalEmisor"])
	d.RfcReceptor = strVal(row["RfcReceptor"])
	d.NombreReceptor = strVal(row["NombreReceptor"])
	d.DomicilioFiscalReceptor = strVal(row["DomicilioFiscalReceptor"])
	d.RegimenFiscalReceptor = strVal(row["RegimenFiscalReceptor"])
	d.NoCertificado = strVal(row["NoCertificado"])
	d.NoCertificadoSAT = strVal(row["NoCertificadoSAT"])
	d.Sello = strVal(row["Sello"])
	d.SelloSAT = strVal(row["SelloSAT"])
	d.RfcPac = strVal(row["RfcPac"])
	d.FormaPago = strVal(row["FormaPago"])
	d.MetodoPago = strVal(row["MetodoPago"])
	d.UsoCFDIReceptor = strVal(row["UsoCFDIReceptor"])
	d.Moneda = strVal(row["Moneda"])

	tipo := strVal(row["TipoDeComprobante"])
	if label, ok := tipoLabels[tipo]; ok {
		d.TipoLabel = label
	} else {
		d.TipoLabel = tipo
	}

	if fc, ok := row["FechaCancelacion"]; ok && fc != nil {
		d.FechaCancelacion = fmt.Sprintf("%v", fc)
	}
	if fecha, ok := row["Fecha"]; ok && fecha != nil {
		d.Fecha = fmt.Sprintf("%v", fecha)
	}
	if fcs, ok := row["FechaCertificacionSat"]; ok && fcs != nil {
		d.FechaCertificacionSat = fmt.Sprintf("%v", fcs)
	}

	d.Total = fmtMoney(row["Total"])
	d.SubTotal = fmtMoney(row["SubTotal"])
	d.TrasladosIVA = fmtMoneyIfPositive(row["TrasladosIVA"])
	d.TrasladosISR = fmtMoneyIfPositive(row["TrasladosISR"])
	d.TrasladosIEPS = fmtMoneyIfPositive(row["TrasladosIEPS"])
	d.RetencionesIVA = fmtMoneyIfPositive(row["RetencionesIVA"])
	d.RetencionesISR = fmtMoneyIfPositive(row["RetencionesISR"])
	d.RetencionesIEPS = fmtMoneyIfPositive(row["RetencionesIEPS"])

	if tc, ok := row["TipoCambio"]; ok && tc != nil {
		moneda := strVal(row["Moneda"])
		if moneda != "" && moneda != "MXN" && moneda != "Pesos" {
			d.TipoCambioLabel = fmt.Sprintf("- Tipo de cambio: %s", fmtMoney(tc))
		}
	}

	if desc, ok := row["Descuento"]; ok && desc != nil {
		v := parseFloatAny(desc)
		if v > 0 {
			d.Descuento = fmt.Sprintf("$%s", formatNumberWithCommas(v))
		}
	}

	// Parse Conceptos JSON
	if conceptosRaw, ok := row["Conceptos"]; ok && conceptosRaw != nil {
		d.Conceptos = parseConceptos(fmt.Sprintf("%v", conceptosRaw))
	}

	return d
}

// parseConceptos deserializes the CFDI Conceptos JSON field into a renderable slice.
func parseConceptos(raw string) []conceptoPDF {
	var outer map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &outer); err != nil {
		return nil
	}

	conceptoRaw, ok := outer["Concepto"]
	if !ok {
		return nil
	}

	// Concepto can be a single object or a list.
	var items []map[string]interface{}
	switch v := conceptoRaw.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				items = append(items, m)
			}
		}
	case map[string]interface{}:
		items = []map[string]interface{}{v}
	}

	var result []conceptoPDF
	for _, item := range items {
		c := conceptoPDF{
			ClaveProdServ: strVal(item["@ClaveProdServ"]),
			Cantidad:      strVal(item["@Cantidad"]),
			ClaveUnidad:   strVal(item["@ClaveUnidad"]),
			Descripcion:   strVal(item["@Descripcion"]),
			ValorUnitario: fmtMoney(item["@ValorUnitario"]),
			Importe:       fmtMoney(item["@Importe"]),
		}
		if u := strVal(item["@Unidad"]); u != "" {
			c.ClaveUnidad += " - " + u
		}
		result = append(result, c)
	}
	return result
}

func strVal(v interface{}) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func boolValPDF(v interface{}) bool {
	if v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.ToLower(b) == "true"
	}
	return false
}

func fmtMoney(v interface{}) string {
	if v == nil {
		return ""
	}
	f := parseFloatAny(v)
	if f == 0 {
		return ""
	}
	return fmt.Sprintf("$%s", formatNumberWithCommas(f))
}

func fmtMoneyIfPositive(v interface{}) string {
	f := parseFloatAny(v)
	if f <= 0 {
		return ""
	}
	return fmt.Sprintf("$%s", formatNumberWithCommas(f))
}

func formatNumberWithCommas(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	parts := strings.Split(s, ".")
	intPart := parts[0]
	negative := strings.HasPrefix(intPart, "-")
	if negative {
		intPart = intPart[1:]
	}
	var result []byte
	for i, c := range []byte(intPart) {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, c)
	}
	out := string(result) + "." + parts[1]
	if negative {
		out = "-" + out
	}
	return out
}
