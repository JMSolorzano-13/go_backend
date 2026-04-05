package cfdi

import (
	"context"
	cryptoRand "crypto/rand"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/xuri/excelize/v2"

	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
)

// PublishExport creates a CfdiExport record and publishes an event for async processing.
func PublishExport(
	ctx context.Context,
	db bun.IDB,
	bus *event.Bus,
	companyIdentifier string,
	period time.Time,
	displayedName string,
	exportFilter string,
	exportDataType string,
	format string,
	isIssued bool,
	isYearly bool,
	exportData map[string]interface{},
	jsonBody map[string]interface{},
) string {
	downloadType := "RECEIVED"
	if isIssued {
		downloadType = "ISSUED"
	}
	fileName := "export"
	if exportData != nil {
		if fn, ok := exportData["file_name"].(string); ok {
			fileName = fn
		}
	}

	// Derive cfdi_type from domain (TipoDeComprobante) + export_data.type
	// to match Python's cfdi_type_format() output (e.g. "I", "Iconceptos", "Pdoctos")
	cfdiType := deriveCfdiType(jsonBody, exportData)

	now := time.Now().UTC()
	state := tenant.ExportStateSent
	export := &tenant.CfdiExport{
		Identifier:      newUUID(),
		DisplayedName:   displayedName,
		State:           &state,
		Start:           strPtr(period.Format("2006-01-02T15:04:05")),
		ExportDataType:  &exportDataType,
		Format:          &format,
		DownloadType:    &downloadType,
		ExternalRequest: &isYearly,
		FileName:        fileName,
		Domain:          strPtr(exportFilter),
		CfdiType:        cfdiType,
		CreatedAt:       now,
	}

	_, err := db.NewInsert().Model(export).Exec(ctx)
	if err != nil {
		return ""
	}

	if bus != nil {
		if exportDataType == tenant.ExportDataTypeCFDI {
			// CFDI exports use queue_export (USER_EXPORT_CREATED), matching Python's
			// export_event() flow which requires cfdi_export_identifier in the body.
			body := make(map[string]interface{}, len(jsonBody)+1)
			for k, v := range jsonBody {
				body[k] = v
			}
			body["cfdi_export_identifier"] = export.Identifier
			bus.Publish(event.EventTypeUserExportCreated, event.SQSMessagePayload{
				SQSBase:           event.NewSQSBase(),
				CompanyIdentifier: companyIdentifier,
				JSONBody:          body,
			})
		} else {
			// IVA/ISR exports use queue_massive_export (MASSIVE_EXPORT_CREATED), matching
			// Python's publish_export() → handle_export_type() flow.
			bus.Publish(event.EventTypeMassiveExportCreated, event.SQSMessagePayload{
				SQSBase:           event.NewSQSBase(),
				CompanyIdentifier: companyIdentifier,
				JSONBody: map[string]interface{}{
					"identifier":  export.Identifier,
					"export_data": exportData,
					"json_body":   jsonBody,
				},
			})
		}
	}

	return export.Identifier
}

// CreateExportRecord creates a CfdiExport record for ISR exports.
func CreateExportRecord(ctx context.Context, db bun.IDB, body map[string]interface{}) *tenant.CfdiExport {
	periodStr, _ := body["period"].(string)
	displayedName, _ := body["displayed_name"].(string)
	issuedRaw, _ := body["issued"].(bool)
	yearlyRaw, _ := body["yearly"].(bool)
	exportDataRaw, _ := body["export_data"].(map[string]interface{})
	fileName := "export"
	if exportDataRaw != nil {
		if fn, ok := exportDataRaw["file_name"].(string); ok {
			fileName = fn
		}
	}

	downloadType := "RECEIVED"
	if issuedRaw {
		downloadType = "ISSUED"
	}

	now := time.Now().UTC()
	state := tenant.ExportStateSent
	dataType := tenant.ExportDataTypeISR
	format := "XLSX"

	export := &tenant.CfdiExport{
		Identifier:      newUUID(),
		DisplayedName:   displayedName,
		State:           &state,
		Start:           &periodStr,
		ExportDataType:  &dataType,
		Format:          &format,
		DownloadType:    &downloadType,
		ExternalRequest: &yearlyRaw,
		FileName:        fileName,
		Domain:          strPtr(""),
		CreatedAt:       now,
	}
	db.NewInsert().Model(export).Exec(ctx)
	return export
}

// SaveExportToS3 uploads export bytes and updates the CfdiExport record.
func SaveExportToS3(ctx context.Context, db bun.IDB, s3c ExportObjectStorage, exportBytes []byte, export *tenant.CfdiExport, exportData map[string]interface{}) {
	if s3c == nil {
		return
	}
	bucket := ""
	key := fmt.Sprintf("exports/%s/%s.xlsx", export.Identifier, export.FileName)

	if exportData != nil {
		if b, ok := exportData["bucket"].(string); ok {
			bucket = b
		}
	}
	if bucket == "" {
		return
	}

	s3c.Upload(ctx, bucket, key, exportBytes)

	url, _ := s3c.PresignGet(ctx, bucket, key, 2*time.Hour)
	expDate := time.Now().UTC().Add(2 * time.Hour)
	toDownload := tenant.ExportStateToDownload
	export.URL = &url
	export.ExpirationDate = &expDate
	export.State = &toDownload
	db.NewUpdate().Model(export).
		Column("url", "expiration_date", "state").
		WherePK().
		Exec(ctx)
}

// ExportISRTotalesXLSX generates the ISR totals Excel workbook.
func ExportISRTotalesXLSX(isrData map[string]interface{}) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Totales"
	f.SetSheetName("Sheet1", sheet)
	f.SetCellValue(sheet, "A1", "")
	f.SetCellValue(sheet, "B1", "Conteo de CFDIs")
	f.SetCellValue(sheet, "C1", "Importe")
	f.SetCellValue(sheet, "D1", "ISR Retenido a Cargo")

	totalsTable, _ := isrData["totals_table"].([]map[string]interface{})
	row := 2
	for _, item := range totalsTable {
		concepto, _ := item["Concepto"].(string)
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), concepto)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), item["ConteoCFDIs"])
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), item["Importe"])
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), item["isr_cargo"])
		row++

		if concepts, ok := item["concepts"].([]map[string]interface{}); ok {
			for _, sub := range concepts {
				subConcepto, _ := sub["Concepto"].(string)
				f.SetCellValue(sheet, fmt.Sprintf("A%d", row), "  "+subConcepto)
				f.SetCellValue(sheet, fmt.Sprintf("B%d", row), sub["ConteoCFDIs"])
				f.SetCellValue(sheet, fmt.Sprintf("C%d", row), sub["Importe"])
				f.SetCellValue(sheet, fmt.Sprintf("D%d", row), sub["isr_cargo"])
				row++
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// deriveCfdiType mirrors Python's cfdi_type_format():
// extracts TipoDeComprobante from domain and appends export_data.type
// for "conceptos" and "doctos" variants (e.g. "Iconceptos", "Pdoctos").
func deriveCfdiType(jsonBody map[string]interface{}, exportData map[string]interface{}) *string {
	tipoDeComprobante := ""
	if domain, ok := jsonBody["domain"].([]interface{}); ok {
		for _, item := range domain {
			if arr, ok := item.([]interface{}); ok && len(arr) == 3 {
				if arr[0] == "TipoDeComprobante" && arr[1] == "=" {
					if v, ok := arr[2].(string); ok {
						tipoDeComprobante = v
					}
				}
			}
		}
	}
	if tipoDeComprobante == "" {
		return nil
	}
	exportType := ""
	if exportData != nil {
		if t, ok := exportData["type"].(string); ok && (t == "conceptos" || t == "doctos") {
			exportType = t
		}
	}
	result := tipoDeComprobante + exportType
	return &result
}

func strPtr(s string) *string { return &s }

func newUUID() string {
	b := make([]byte, 16)
	_, _ = cryptoRand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
