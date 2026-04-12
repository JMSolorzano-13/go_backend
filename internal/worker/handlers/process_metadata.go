package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/domain/event"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// metadataRecord is a parsed metadata row from the SAT TXT file.
type metadataRecord struct {
	UUID                   string
	RfcEmisor              string
	NombreEmisor           string
	RfcReceptor            string
	NombreReceptor         string
	RfcPac                 string
	FechaEmision           string
	FechaCertificacionSat  string
	Monto                  string
	EfectoComprobante      string // First letter → TipoDeComprobante
	Estatus                string // "Vigente" | "Cancelado"
	FechaCancelacion       string
}

// metadataTXTFieldCount is the expected number of fields per TXT row.
const metadataTXTFieldCount = 12

// ProcessMetadata handles SAT_METADATA_DOWNLOADED messages.
//
// Pipeline step 4: download package ZIPs from blob → extract metadata TXT →
// parse records → bulk upsert into cfdi table → cascade cancel related docs →
// update query state=PROCESSED → publish SAT_COMPLETE_CFDIS_NEEDED.
//
// Mirrors Python MetadataProcessor.process.
type ProcessMetadata struct {
	Deps
}

func (h *ProcessMetadata) Handle(ctx context.Context, raw json.RawMessage) error {
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
		"handler", "ProcessMetadata",
		"company", msg.CompanyIdentifier,
		"query", msg.QueryIdentifier,
		"packages", len(msg.Packages),
	)

	// Determine company RFC by looking at the company in control DB.
	companyRFC, err := h.getCompanyRFC(ctx, msg.CompanyIdentifier)
	if err != nil {
		return fmt.Errorf("get company RFC: %w", err)
	}

	// Process each package.
	totalUpserted := 0
	for _, pkgID := range msg.Packages {
		records, parseErr := h.extractMetadataFromPackage(ctx, pkgID)
		if parseErr != nil {
			logger.Error("failed to extract metadata from package", "package", pkgID, "error", parseErr)
			continue
		}

		n, upsertErr := h.upsertMetadataRecords(ctx, conn, records, msg.CompanyIdentifier, companyRFC)
		if upsertErr != nil {
			logger.Error("failed to upsert metadata", "package", pkgID, "error", upsertErr)
			continue
		}

		totalUpserted += n
		logger.Info("package metadata processed", "package", pkgID, "records", len(records), "upserted", n)
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

	logger.Info("metadata processing complete", "total_upserted", totalUpserted)

	// Publish SAT_COMPLETE_CFDIS_NEEDED → triggers CompleteCFDIs to fill XML gaps.
	h.Bus.Publish(event.EventTypeSATCompleteCFDIsNeeded, event.NeedToCompleteCFDIsEvent{
		CompanyBase:  event.NewCompanyBase(msg.CompanyIdentifier, companyRFC),
		DownloadType: msg.DownloadType,
		IsManual:     msg.IsManual,
		Start:        &msg.Start,
		End:          &msg.End,
	})

	return nil
}

// extractMetadataFromPackage downloads a ZIP from blob, extracts the .txt file,
// and parses metadata records.
func (h *ProcessMetadata) extractMetadataFromPackage(ctx context.Context, packageID string) ([]metadataRecord, error) {
	bucket := h.Cfg.S3Attachments
	blobKey := fmt.Sprintf("attachments/Zips/%s.zip", packageID)

	zipData, err := h.Storage.Download(ctx, bucket, blobKey)
	if err != nil {
		return nil, fmt.Errorf("download package ZIP: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("open ZIP: %w", err)
	}

	// Find the metadata .txt file (not *_tercero.txt).
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".txt") && !strings.HasSuffix(f.Name, "_tercero.txt") {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open txt: %w", err)
			}
			defer rc.Close()

			reader := csv.NewReader(rc)
			reader.Comma = '~'
			reader.LazyQuotes = true
			reader.FieldsPerRecord = -1 // variable fields

			records, err := reader.ReadAll()
			if err != nil {
				return nil, fmt.Errorf("read CSV: %w", err)
			}

			return parseMetadataRows(records), nil
		}
	}

	return nil, fmt.Errorf("no metadata .txt file in package %s", packageID)
}

// parseMetadataRows parses CSV rows from the SAT metadata TXT file.
// The first row is the header; subsequent rows have ~-delimited fields.
// Matches Python Metadata.from_txt multi-line joining logic.
func parseMetadataRows(rows [][]string) []metadataRecord {
	if len(rows) < 2 {
		return nil
	}

	var records []metadataRecord
	seen := make(map[string]bool)

	// Skip header row.
	var tokens []string
	for _, row := range rows[1:] {
		if len(row) == metadataTXTFieldCount-1 {
			row = append(row, "") // missing FechaCancelacion
		}

		if len(row) == metadataTXTFieldCount {
			tokens = row
		} else {
			// Multi-line field: join with previous tokens.
			if len(tokens) > 0 && len(row) > 0 {
				tokens[len(tokens)-1] += row[0]
				tokens = append(tokens, row[1:]...)
			} else {
				tokens = append(tokens, row...)
			}
			if len(tokens) != metadataTXTFieldCount {
				continue
			}
		}

		rec := metadataRecord{
			UUID:                  strings.ToLower(strings.TrimSpace(tokens[0])),
			RfcEmisor:             tokens[1],
			NombreEmisor:          tokens[2],
			RfcReceptor:           tokens[3],
			NombreReceptor:        tokens[4],
			RfcPac:                tokens[5],
			FechaEmision:          tokens[6],
			FechaCertificacionSat: tokens[7],
			Monto:                 tokens[8],
			EfectoComprobante:     tokens[9],
			Estatus:               tokens[10],
			FechaCancelacion:      tokens[11],
		}

		// Deduplicate by UUID.
		if seen[rec.UUID] {
			tokens = nil
			continue
		}
		seen[rec.UUID] = true
		records = append(records, rec)
		tokens = nil
	}

	return records
}

// upsertMetadataRecords inserts metadata into the cfdi table using INSERT ON CONFLICT.
// Returns the number of rows affected.
func (h *ProcessMetadata) upsertMetadataRecords(ctx context.Context, conn bun.Conn, records []metadataRecord, companyID, companyRFC string) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	var cancelledUUIDs []string

	// Process in batches to avoid huge SQL statements.
	const batchSize = 500
	totalAffected := 0

	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]

		cfdis := make([]tenant.CFDI, 0, len(batch))
		for _, rec := range batch {
			isIssued := rec.RfcEmisor == companyRFC
			estatus := rec.Estatus == "Vigente" || rec.Estatus == "1" || rec.Estatus == "t"
			otherRFC := rec.RfcReceptor
			if isIssued {
				otherRFC = rec.RfcReceptor
			} else {
				otherRFC = rec.RfcEmisor
			}

			fecha := parseDatetime(rec.FechaEmision)
			total := parseFloat(rec.Monto)
			tipoComprobante := ""
			if len(rec.EfectoComprobante) > 0 {
				tipoComprobante = strings.ToUpper(rec.EfectoComprobante[:1])
			}

			var fechaCancelacion *time.Time
			if rec.FechaCancelacion != "" {
				t := parseDatetime(rec.FechaCancelacion)
				if !t.IsZero() {
					fechaCancelacion = &t
				}
			}

			if !estatus {
				cancelledUUIDs = append(cancelledUUIDs, rec.UUID)
			}

			cfdi := tenant.CFDI{
				CompanyIdentifier:     companyID,
				IsIssued:              isIssued,
				UUID:                  rec.UUID,
				Fecha:                 fecha,
				Total:                 total,
				TipoDeComprobante:     tipoComprobante,
				RfcEmisor:             rec.RfcEmisor,
				NombreEmisor:          &rec.NombreEmisor,
				RfcReceptor:           rec.RfcReceptor,
				NombreReceptor:        &rec.NombreReceptor,
				RfcPac:                &rec.RfcPac,
				FechaCertificacionSat: parseDatetime(rec.FechaCertificacionSat),
				Estatus:               estatus,
				FechaCancelacion:      fechaCancelacion,
				FechaFiltro:           fecha,
				PaymentDate:           fecha,
				OtherRFC:              &otherRFC,
				Active:                true,
				FromXML:               false,
				CreatedAt:             now,
				UpdatedAt:             now,
			}

			cfdis = append(cfdis, cfdi)
		}

		// INSERT ... ON CONFLICT (company_identifier, is_issued, "UUID") DO UPDATE
		// Only update if the existing record is Vigente (Estatus=true) and the new one is Cancelado.
		res, err := conn.NewInsert().
			Model(&cfdis).
			On(`CONFLICT (company_identifier, is_issued, "UUID") DO UPDATE`).
			Set(`"Estatus" = EXCLUDED."Estatus"`).
			Set(`"FechaCancelacion" = EXCLUDED."FechaCancelacion"`).
			Set("updated_at = EXCLUDED.updated_at").
			Where(`cfdi."Estatus" = true AND EXCLUDED."Estatus" = false`).
			Exec(ctx)
		if err != nil {
			return totalAffected, fmt.Errorf("upsert batch: %w", err)
		}

		n, _ := res.RowsAffected()
		totalAffected += int(n)
	}

	// Cascade cancellations to related tables.
	if len(cancelledUUIDs) > 0 {
		h.cancelRelated(ctx, conn, cancelledUUIDs)
	}

	return totalAffected, nil
}

// cancelRelated marks related DoctoRelacionado, Payment, and CfdiRelacionado
// rows as Estatus=false when the parent CFDI is cancelled.
func (h *ProcessMetadata) cancelRelated(ctx context.Context, conn bun.Conn, uuids []string) {
	// docto_relacionado — has UUID column
	if _, err := conn.NewUpdate().
		TableExpr("docto_relacionado").
		Set(`"Estatus" = false`).
		Where(`"UUID" IN (?)`, bun.In(uuids)).
		Where(`"Estatus" = true`).
		Exec(ctx); err != nil {
		slog.Error("cancel docto_relacionado failed", "error", err)
	}

	// payment — has uuid_origin column
	if _, err := conn.NewUpdate().
		TableExpr("payment").
		Set(`"Estatus" = false`).
		Where("uuid_origin IN (?)", bun.In(uuids)).
		Where(`"Estatus" = true`).
		Exec(ctx); err != nil {
		slog.Error("cancel payment failed", "error", err)
	}

	// cfdi_relacionado — has uuid_origin column
	if _, err := conn.NewUpdate().
		TableExpr("cfdi_relacionado").
		Set(`"Estatus" = false`).
		Where("uuid_origin IN (?)", bun.In(uuids)).
		Where(`"Estatus" = true`).
		Exec(ctx); err != nil {
		slog.Error("cancel cfdi_relacionado failed", "error", err)
	}
}

// getCompanyRFC looks up the company RFC from the control database.
func (h *ProcessMetadata) getCompanyRFC(ctx context.Context, companyIdentifier string) (string, error) {
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

// parseDatetime parses a SAT datetime string (ISO 8601 or similar).
func parseDatetime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}

	formats := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.000",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseFloat parses a money string like "$1,234.56" to float64.
func parseFloat(s string) float64 {
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
