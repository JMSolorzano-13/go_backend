package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/domain/datetime"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/model/control"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// maxCFDIPerChunk is the maximum number of CFDIs per date-range chunk when
// creating XML download queries.
const maxCFDIPerChunk = 10_000

// CompleteCFDIs handles SAT_COMPLETE_CFDIS_NEEDED messages.
//
// Pipeline step 6: query CFDIs that have metadata but no XML (from_xml=false,
// Estatus=true, is_too_big=false), split them into date-range chunks, and
// publish SAT_WS_REQUEST_CREATE_QUERY for each chunk to download the XMLs.
//
// Mirrors Python QueryCFDISCompleter.complete_cfdis + get_cfdi_chunks.
type CompleteCFDIs struct {
	Deps
}

func (h *CompleteCFDIs) Handle(ctx context.Context, raw json.RawMessage) error {
	var msg CompleteCFDIsMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("unmarshal CompleteCFDIsMsg: %w", err)
	}

	logger := slog.With(
		"handler", "CompleteCFDIs",
		"company", msg.CompanyIdentifier,
		"download_type", msg.DownloadType,
	)

	// Determine date range.
	now := time.Now().UTC()
	start := derefTime(msg.Start, datetime.LastXFiscalYearsStart(5))
	end := derefTime(msg.End, now)

	// Get company to look up wid/cid.
	var company control.Company
	if err := h.DB.Primary.NewSelect().
		Model(&company).
		Where("identifier = ?", msg.CompanyIdentifier).
		Scan(ctx); err != nil {
		return fmt.Errorf("lookup company: %w", err)
	}

	wid := int64(0)
	if company.WorkspaceID != nil {
		wid = *company.WorkspaceID
	}

	conn, err := h.DB.TenantConn(ctx, msg.CompanyIdentifier, true)
	if err != nil {
		return fmt.Errorf("tenant conn: %w", err)
	}
	defer conn.Close()

	isIssued := msg.DownloadType == tenant.DownloadTypeIssued

	// Find date range of CFDIs needing XML.
	chunks, err := h.getCFDIChunks(ctx, conn, isIssued, start, end)
	if err != nil {
		return fmt.Errorf("get CFDI chunks: %w", err)
	}

	if len(chunks) == 0 {
		logger.Info("no CFDIs need XML download")
		return nil
	}

	logger.Info("publishing create-query events for CFDI chunks", "chunks", len(chunks))

	for _, chunk := range chunks {
		chunkStart := chunk.start
		chunkEnd := chunk.end
		h.Bus.Publish(event.EventTypeSATWSRequestCreateQuery, event.QueryCreateEvent{
			SQSBase:           event.NewSQSBase(),
			CompanyIdentifier: msg.CompanyIdentifier,
			DownloadType:      msg.DownloadType,
			RequestType:       tenant.RequestTypeCFDI,
			IsManual:          msg.IsManual,
			Start:             &chunkStart,
			End:               &chunkEnd,
			WID:               wid,
			CID:               company.ID,
		})
	}

	logger.Info("complete_cfdis done", "chunks_published", len(chunks))
	return nil
}

// dateChunk is a date range for a CFDI XML download query.
type dateChunk struct {
	start time.Time
	end   time.Time
}

// getCFDIChunks queries CFDIs that need XML (Estatus=true, from_xml=false,
// is_too_big=false) and splits them into date-range chunks of maxCFDIPerChunk.
//
// Mirrors Python get_chunks_need_xml: queries all Fecha values, walks them in
// order, and creates chunks of up to maxCFDIPerChunk records.
func (h *CompleteCFDIs) getCFDIChunks(ctx context.Context, conn bun.Conn, isIssued bool, start, end time.Time) ([]dateChunk, error) {
	// Get min/max dates of CFDIs needing XML.
	var result struct {
		MinFecha time.Time `bun:"min_fecha"`
		MaxFecha time.Time `bun:"max_fecha"`
	}

	err := conn.NewSelect().
		TableExpr("cfdi").
		ColumnExpr(`MIN("Fecha") AS min_fecha`).
		ColumnExpr(`MAX("Fecha") AS max_fecha`).
		Where(`"Fecha" BETWEEN ? AND ?`, start, end).
		Where("is_issued = ?", isIssued).
		Where(`"Estatus" = true`).
		Where("from_xml = false").
		Where("is_too_big = false").
		Scan(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf("query min/max fecha: %w", err)
	}

	if result.MinFecha.IsZero() || result.MaxFecha.IsZero() {
		return nil, nil
	}

	// Query all dates (with flag indicating need-download) ordered by Fecha.
	type dateRow struct {
		Fecha        time.Time `bun:"Fecha"`
		NeedDownload bool      `bun:"need_download"`
	}

	var dates []dateRow
	err = conn.NewSelect().
		TableExpr("cfdi").
		ColumnExpr(`"Fecha"`).
		ColumnExpr(`("Estatus" AND NOT from_xml AND NOT is_too_big) AS need_download`).
		Where(`"Fecha" BETWEEN ? AND ?`, result.MinFecha, result.MaxFecha).
		Where("is_issued = ?", isIssued).
		OrderExpr(`"Fecha" ASC`).
		Scan(ctx, &dates)
	if err != nil {
		return nil, fmt.Errorf("query dates: %w", err)
	}

	if len(dates) == 0 {
		return nil, nil
	}

	// Walk dates creating chunks of maxCFDIPerChunk.
	var chunks []dateChunk
	maxIx := len(dates) - 1
	ixStart := 0

	for ixStart <= maxIx {
		// Advance ixStart past non-downloadable dates.
		for ixStart < maxIx && !dates[ixStart].NeedDownload {
			ixStart++
		}

		ixEnd := ixStart + maxCFDIPerChunk - 1
		if ixEnd > maxIx {
			ixEnd = maxIx
		}
		lastEnd := ixEnd

		// Retract ixEnd past non-downloadable dates.
		for ixEnd > ixStart && !dates[ixEnd].NeedDownload {
			ixEnd--
		}

		if dates[ixStart].NeedDownload {
			chunks = append(chunks, dateChunk{
				start: dates[ixStart].Fecha,
				end:   dates[ixEnd].Fecha,
			})
		}

		ixStart = lastEnd + 1
	}

	return chunks, nil
}
