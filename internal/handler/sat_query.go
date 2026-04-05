package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

type SATQuery struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
}

func NewSATQuery(cfg *config.Config, database *db.Database, bus *event.Bus) *SATQuery {
	return &SATQuery{cfg: cfg, database: database, bus: bus}
}

var satQueryMeta = crud.ModelMeta{
	DefaultOrderBy: `"created_at" DESC`,
	ActiveColumn:   "",
}

// 1. POST /api/SATQuery/search
func (h *SATQuery) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}
	conn, err := database.TenantConn(ctx, cid, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	params, _, err := crud.ParseSearchBodyJSON(raw)
	if err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	result, err := crud.Search[tenant.SATQuery](ctx, conn, params, satQueryMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

// 2. POST /api/SATQuery/manual
func (h *SATQuery) Manual(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := auth.UserFromContext(ctx)
	company, _ := auth.CompanyFromContext(ctx)
	cid, _ := auth.CompanyIdentifierFromContext(ctx)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, cid, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	resp := h.canRequestManualSync(ctx, conn, user, company)

	if resp["status"] != "ok" {
		response.WriteJSON(w, http.StatusOK, resp)
		return
	}

	reverifyCount, err := h.publishReverifications(ctx, conn, company)
	if err != nil {
		slog.Error("sat_query: reverification error", "error", err)
	}
	resp["reverifications"] = reverifyCount

	mxNow := mxNowTime()
	start := mxNow.Add(-h.cfg.ManualRequestStartDelta())

	wid := int64(0)
	if company.WorkspaceID != nil {
		wid = *company.WorkspaceID
	}

	h.bus.Publish(event.EventTypeSATWSRequestCreateQuery, event.QueryCreateEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: company.Identifier,
		DownloadType:      "ISSUED",
		RequestType:       "CFDI",
		IsManual:          true,
		Start:             &start,
		End:               &mxNow,
		WID:               wid,
		CID:               company.ID,
	})

	issuedEnd := mxNow
	h.bus.Publish(event.EventTypeSATWSRequestCreateQuery, event.QueryCreateEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: company.Identifier,
		DownloadType:      "RECEIVED",
		RequestType:       "CFDI",
		IsManual:          true,
		Start:             &start,
		End:               &issuedEnd,
		WID:               wid,
		CID:               company.ID,
	})

	rfc := ""
	if company.RFC != nil {
		rfc = *company.RFC
	}
	h.bus.Publish(event.EventTypeRequestScrap, event.ScrapRequestEvent{
		CompanyIdentifier: company.Identifier,
		CompanyRFC:        rfc,
		WorkspaceID:       wid,
		CompanyID:         company.ID,
	})

	response.WriteJSON(w, http.StatusOK, resp)
}

// 3. POST /api/SATQuery/can_manual_request
func (h *SATQuery) CanManualRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := auth.UserFromContext(ctx)
	company, _ := auth.CompanyFromContext(ctx)
	cid, _ := auth.CompanyIdentifierFromContext(ctx)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, cid, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	resp := h.canRequestManualSync(ctx, conn, user, company)
	response.WriteJSON(w, http.StatusOK, resp)
}

// 4. POST /api/SATQuery/log
func (h *SATQuery) Log(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, cid, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	startStr, _ := body["start"].(string)
	endStr, _ := body["end"].(string)
	if startStr == "" || endStr == "" {
		response.BadRequest(w, "start and end are required")
		return
	}

	startDate, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		response.BadRequest(w, "invalid start date format")
		return
	}
	endDate, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		response.BadRequest(w, "invalid end date format")
		return
	}

	result, err := getCFDIStatusLog(ctx, conn, startDate, endDate)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("cfdi status log: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

// 5. POST /api/SATQuery/massive_scrap
func (h *SATQuery) MassiveScrap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := auth.UserFromContext(ctx)

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	var body map[string]interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}

	companies, err := h.listCompaniesForUser(ctx, user)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("list companies: %v", err))
		return
	}

	published := make([]string, 0)
	skipped := make([]string, 0)

	for _, comp := range companies {
		wid := int64(0)
		if comp.WorkspaceID != nil {
			wid = *comp.WorkspaceID
		}
		rfc := ""
		if comp.RFC != nil {
			rfc = *comp.RFC
		}

		conn, err := h.database.TenantConn(ctx, comp.Identifier, false)
		if err != nil {
			skipped = append(skipped, comp.Identifier)
			continue
		}

		canPublish := h.canPublishScrap(ctx, conn, user, &comp)
		conn.Close()

		if !canPublish {
			skipped = append(skipped, comp.Identifier)
			continue
		}

		h.bus.Publish(event.EventTypeRequestScrap, event.ScrapRequestEvent{
			CompanyIdentifier: comp.Identifier,
			CompanyRFC:        rfc,
			WorkspaceID:       wid,
			CompanyID:         comp.ID,
		})
		published = append(published, comp.Identifier)
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"companies": map[string]interface{}{
			"published": published,
			"skipped":   skipped,
			"total":     len(companies),
		},
	})
}

// canRequestManualSync checks daily limits, permissions, and in-progress queries.
func (h *SATQuery) canRequestManualSync(
	ctx context.Context, conn bun.Conn, user *control.User, company *control.Company,
) map[string]interface{} {
	resp := map[string]interface{}{
		"status":                     "ok",
		"reason":                     "",
		"can_request":                true,
		"last_manual_sync_requested": nil,
		"last_sync_processed":        nil,
		"all_cfdis_processed":        true,
	}

	if company.Active != nil && !*company.Active {
		resp["status"] = "error"
		resp["reason"] = "La empresa no esta activa"
		resp["can_request"] = false
		return resp
	}

	if company.HaveCertificates != nil && !*company.HaveCertificates {
		resp["status"] = "error"
		resp["reason"] = "La empresa no tiene certificados cargados"
		resp["can_request"] = false
		return resp
	}

	var lastManual *time.Time
	var sq tenant.SATQuery
	err := conn.NewSelect().
		Model(&sq).
		Where("is_manual = true").
		Where("request_type = 'METADATA'").
		OrderExpr("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err == nil {
		t := sq.CreatedAt
		lastManual = &t
		resp["last_manual_sync_requested"] = t.Format(crud.APITimestampFormat)
	}

	var lastProcessed tenant.SATQuery
	err = conn.NewSelect().
		Model(&lastProcessed).
		Where("state = 'PROCESSED'").
		OrderExpr("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err == nil {
		resp["last_sync_processed"] = lastProcessed.CreatedAt.Format(crud.APITimestampFormat)
	}

	var incompleteCFDIs int
	incompleteCFDIs, _ = conn.NewSelect().
		TableExpr("cfdi").
		Where(`"Estatus" = true`).
		Where("from_xml = false").
		Where("is_too_big = false").
		Count(ctx)
	resp["all_cfdis_processed"] = incompleteCFDIs == 0

	todayStart := todayMXInUTC()
	manualToday, _ := conn.NewSelect().
		Model((*tenant.SATQuery)(nil)).
		Where("is_manual = true").
		Where("created_at >= ?", todayStart).
		Count(ctx)

	maxPerDay := h.cfg.MaxManualSyncPerDay
	if maxPerDay <= 0 {
		maxPerDay = 3
	}
	if manualToday >= maxPerDay {
		resp["status"] = "error"
		resp["reason"] = "Se ha alcanzado el limite de sincronizaciones manuales por dia"
		resp["can_request"] = false
		return resp
	}

	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	inProgress, _ := conn.NewSelect().
		Model((*tenant.SATQuery)(nil)).
		Where("state NOT IN (?)", bun.In(finalStates)).
		Where("created_at >= ?", twoHoursAgo).
		Count(ctx)
	if inProgress > 0 {
		resp["status"] = "error"
		resp["reason"] = "Hay sincronizaciones en progreso"
		resp["can_request"] = false
	}

	_ = lastManual
	return resp
}

func (h *SATQuery) publishReverifications(ctx context.Context, conn bun.Conn, company *control.Company) (int, error) {
	delta := h.cfg.ManualRequestStartDelta()
	cutoff := time.Now().UTC().Add(-delta)

	var queries []tenant.SATQuery
	err := conn.NewSelect().
		Model(&queries).
		Where("state IN (?)", bun.In(reVerifyStates)).
		Where("created_at >= ?", cutoff).
		Scan(ctx)
	if err != nil {
		return 0, err
	}

	wid := int64(0)
	if company.WorkspaceID != nil {
		wid = *company.WorkspaceID
	}

	for _, q := range queries {
		h.bus.Publish(event.EventTypeSATWSQueryVerifyNeeded, event.QueryVerifyEvent{
			SQSBase:           event.NewSQSBase(),
			CompanyIdentifier: company.Identifier,
			QueryIdentifier:   q.Identifier,
			DownloadType:      q.DownloadType,
			RequestType:       q.RequestType,
			Start:             q.Start,
			End:               q.End,
			State:             q.State,
			Name:              q.Name,
			IsManual:          q.IsManual != nil && *q.IsManual,
			WID:               wid,
			CID:               company.ID,
		})
	}
	return len(queries), nil
}

func (h *SATQuery) listCompaniesForUser(ctx context.Context, user *control.User) ([]control.Company, error) {
	var companies []control.Company
	err := h.database.Primary.NewSelect().
		Model(&companies).
		Join("JOIN workspace AS w ON w.id = c.workspace_id").
		Join("LEFT JOIN permission AS p ON p.company_id = c.id").
		Where("c.active = true").
		Where("w.valid_until IS NOT NULL AND w.valid_until > NOW()").
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.
				Where("w.owner_id = ?", user.ID).
				WhereOr("(p.user_id = ? AND p.role IN (?, ?))", user.ID, control.RoleOperator, control.RolePayroll)
		}).
		Distinct().
		Column("c.id", "c.identifier", "c.workspace_id", "c.rfc", "c.active", "c.have_certificates").
		Scan(ctx)
	return companies, err
}

func (h *SATQuery) canPublishScrap(ctx context.Context, conn bun.Conn, user *control.User, company *control.Company) bool {
	resp := h.canRequestManualSync(ctx, conn, user, company)
	return resp["status"] == "ok"
}

// getCFDIStatusLog matches fastapi_backend/chalicelib/new/cfdi_status_logger (Python):
// JSON keys date + issued/received.processed for the React SAT log; daily status uses
// sat_query range coverage + CFDI counts like Python.
func getCFDIStatusLog(ctx context.Context, conn bun.Conn, startDate, endDate time.Time) (map[string]interface{}, error) {
	startStr := startDate.Format("2006-01-02")
	endStr := endDate.Format("2006-01-02")

	type dailyStatusRow struct {
		FechaDate         time.Time `bun:"fecha_date"`
		TotalIssued       int       `bun:"total_issued"`
		ProcessedIssued   int       `bun:"processed_issued"`
		TotalReceived     int       `bun:"total_received"`
		ProcessedReceived int       `bun:"processed_received"`
		Status            string    `bun:"status"`
	}

	var rows []dailyStatusRow
	err := conn.NewRaw(`
WITH days AS (
	SELECT generate_series(?::date, ?::date, interval '1 day')::date AS fecha_date
),
mr_issued AS (
	SELECT range_agg(tsrange("start", "end", '[)')) AS r
	FROM sat_query
	WHERE state IN ('PROCESSED', 'INFORMATION_NOT_FOUND')
	  AND request_type <> 'CANCELLATION'
	  AND (download_type = 'ISSUED' OR download_type = 'BOTH')
),
mr_received AS (
	SELECT range_agg(tsrange("start", "end", '[)')) AS r
	FROM sat_query
	WHERE state IN ('PROCESSED', 'INFORMATION_NOT_FOUND')
	  AND request_type <> 'CANCELLATION'
	  AND (download_type = 'RECEIVED' OR download_type = 'BOTH')
),
cfdi_stats_issued AS (
	SELECT
		"Fecha"::date AS fecha_date,
		COUNT(*)::int AS total,
		COUNT(*) FILTER (WHERE from_xml OR is_too_big)::int AS processed
	FROM cfdi
	WHERE "Estatus" = true
	  AND is_issued = true
	  AND "Fecha"::date >= ?::date
	  AND "Fecha"::date <= ?::date
	GROUP BY "Fecha"::date
),
cfdi_stats_received AS (
	SELECT
		"Fecha"::date AS fecha_date,
		COUNT(*)::int AS total,
		COUNT(*) FILTER (WHERE from_xml OR is_too_big)::int AS processed
	FROM cfdi
	WHERE "Estatus" = true
	  AND is_issued = false
	  AND "Fecha"::date >= ?::date
	  AND "Fecha"::date <= ?::date
	GROUP BY "Fecha"::date
),
computed AS (
	SELECT
		d.fecha_date,
		COALESCE(i.total, 0) AS total_issued,
		COALESCE(i.processed, 0) AS processed_issued,
		COALESCE(r.total, 0) AS total_received,
		COALESCE(r.processed, 0) AS processed_received,
		CASE
			WHEN COALESCE(i.total, 0) > COALESCE(i.processed, 0) THEN 'INCOMPLETE'
			WHEN NOT COALESCE(
				tsrange(d.fecha_date::timestamp, (d.fecha_date + interval '1 day')::timestamp, '[)') <@ mi.r,
				false
			) THEN 'INCOMPLETE'
			WHEN COALESCE(i.total, 0) = 0 THEN 'EMPTY'
			ELSE 'COMPLETE'
		END AS status_issued,
		CASE
			WHEN COALESCE(r.total, 0) > COALESCE(r.processed, 0) THEN 'INCOMPLETE'
			WHEN NOT COALESCE(
				tsrange(d.fecha_date::timestamp, (d.fecha_date + interval '1 day')::timestamp, '[)') <@ mr_cov.r,
				false
			) THEN 'INCOMPLETE'
			WHEN COALESCE(r.total, 0) = 0 THEN 'EMPTY'
			ELSE 'COMPLETE'
		END AS status_received
	FROM days d
	LEFT JOIN cfdi_stats_issued i ON i.fecha_date = d.fecha_date
	LEFT JOIN cfdi_stats_received r ON r.fecha_date = d.fecha_date
	CROSS JOIN mr_issued mi
	CROSS JOIN mr_received mr_cov
)
SELECT
	fecha_date,
	total_issued,
	processed_issued,
	total_received,
	processed_received,
	CASE
		WHEN status_issued = 'INCOMPLETE' OR status_received = 'INCOMPLETE' THEN 'INCOMPLETE'
		WHEN status_issued = 'COMPLETE' AND status_received = 'COMPLETE' THEN 'COMPLETE'
		WHEN status_issued = 'EMPTY' AND status_received = 'EMPTY' THEN 'EMPTY'
		ELSE 'COMPLETE'
	END AS status
FROM computed
ORDER BY fecha_date DESC
	`, startStr, endStr, startStr, endStr, startStr, endStr).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}

	days := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		days = append(days, map[string]interface{}{
			"date":   row.FechaDate.Format("2006-01-02"),
			"status": row.Status,
			"issued": map[string]interface{}{
				"total":     row.TotalIssued,
				"processed": row.ProcessedIssued,
			},
			"received": map[string]interface{}{
				"total":     row.TotalReceived,
				"processed": row.ProcessedReceived,
			},
		})
	}

	historic, err := calculateHistoricCFDIStatus(ctx, conn)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"days":     days,
		"historic": historic,
	}, nil
}

// calculateHistoricCFDIStatus mirrors Python _calculate_historic (metadata window + CFDI totals).
func calculateHistoricCFDIStatus(ctx context.Context, conn bun.Conn) (map[string]interface{}, error) {
	var first tenant.SATQuery
	err := conn.NewSelect().
		Model(&first).
		Where("request_type = ?", tenant.RequestTypeMetadata).
		Order("created_at ASC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}

	startLog := utcTimestampToMXCalendarDate(first.Start)

	var last tenant.SATQuery
	err = conn.NewSelect().
		Model(&last).
		Where("request_type = ?", tenant.RequestTypeMetadata).
		Where("state IN (?)", bun.In([]string{"PROCESSED", "INFORMATION_NOT_FOUND"})).
		OrderExpr(`"end" DESC`).
		Limit(1).
		Scan(ctx)
	endLog := startLog
	if err == nil {
		endLog = utcTimestampToMXCalendarDate(last.End)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	type histRow struct {
		IsIssued  bool `bun:"is_issued"`
		Total     int  `bun:"total"`
		Processed int  `bun:"processed"`
	}
	var histRows []histRow
	err = conn.NewRaw(`
		SELECT is_issued,
			COUNT(*)::int AS total,
			COUNT(*) FILTER (WHERE from_xml OR is_too_big)::int AS processed
		FROM cfdi
		WHERE "Estatus" = true
		  AND "Fecha"::date >= ?::date
		  AND "Fecha"::date <= ?::date
		GROUP BY is_issued
	`, startLog, endLog).Scan(ctx, &histRows)
	if err != nil {
		return nil, err
	}

	issued := map[string]interface{}{"total": 0, "processed": 0}
	received := map[string]interface{}{"total": 0, "processed": 0}
	for _, h := range histRows {
		if h.IsIssued {
			issued["total"] = h.Total
			issued["processed"] = h.Processed
		} else {
			received["total"] = h.Total
			received["processed"] = h.Processed
		}
	}

	it, _ := issued["total"].(int)
	ip, _ := issued["processed"].(int)
	rt, _ := received["total"].(int)
	rp, _ := received["processed"].(int)
	status := "COMPLETE"
	if it != ip || rt != rp {
		status = "INCOMPLETE"
	}

	return map[string]interface{}{
		"start":    startLog,
		"end":      endLog,
		"status":   status,
		"issued":   issued,
		"received": received,
	}, nil
}

func utcTimestampToMXCalendarDate(t time.Time) string {
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		return t.UTC().Format("2006-01-02")
	}
	return t.UTC().In(loc).Format("2006-01-02")
}

func mxNowTime() time.Time {
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		return time.Now().UTC().Add(-6 * time.Hour)
	}
	return time.Now().In(loc)
}

func todayMXInUTC() time.Time {
	mx := mxNowTime()
	y, m, d := mx.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, mx.Location()).UTC()
}

var finalStates = []string{
	"PROCESSED", "ERROR", "CANT_SCRAP", "SPLITTED", "MANUALLY_CANCELLED",
	"INFORMATION_NOT_FOUND", "SUBSTITUTED", "SCRAPPED",
	"ERROR_IN_CERTS", "ERROR_SAT_WS_UNKNOWN", "ERROR_SAT_WS_INTERNAL",
	"ERROR_TOO_BIG", "SCRAP_FAILED",
}

var reVerifyStates = []string{"SENT", "TIME_LIMIT_REACHED"}
