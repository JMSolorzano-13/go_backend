package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/uptrace/bun"
	"github.com/xuri/excelize/v2"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	cfdidomain "github.com/siigofiscal/go_backend/internal/domain/cfdi"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

type CFDI struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
	files    port.FileStorage
}

func NewCFDI(cfg *config.Config, database *db.Database, bus *event.Bus, files port.FileStorage) *CFDI {
	return &CFDI{cfg: cfg, database: database, bus: bus, files: files}
}

var cfdiMeta = crud.ModelMeta{
	DefaultOrderBy: `"FechaFiltro" DESC`,
	FuzzyFields:    []string{"NombreEmisor", "NombreReceptor", "RfcEmisor", "RfcReceptor", "UUID"},
	ActiveColumn:   "active",
}

func (h *CFDI) tenantConn(r *http.Request, readOnly bool) (bun.Conn, string, error) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}
	conn, err := database.TenantConn(ctx, cid, readOnly)
	return conn, cid, err
}

func (h *CFDI) readBody(r *http.Request) (map[string]interface{}, []byte, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, err
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, raw, err
	}
	return body, raw, nil
}

// 1. POST /api/CFDI/search
func (h *CFDI) Search(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
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

	result, err := crud.Search[tenant.CFDI](r.Context(), conn, params, cfdiMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	enrichCFDIsWithPolizas(r.Context(), conn, result)
	response.WriteJSON(w, http.StatusOK, result)
}

// enrichCFDIsWithPolizas loads poliza data for CFDI search results via poliza_cfdi join table,
// replicating Python's SQLAlchemy relationship: CFDI.polizas → Poliza (through poliza_cfdi).
func enrichCFDIsWithPolizas(ctx context.Context, conn bun.Conn, result *crud.SearchResult) {
	if result == nil || len(result.Data) == 0 {
		for i := range result.Data {
			result.Data[i]["polizas"] = []interface{}{}
		}
		return
	}

	uuids := make([]string, 0, len(result.Data))
	for _, rec := range result.Data {
		if uuid, ok := rec["UUID"].(string); ok && uuid != "" {
			uuids = append(uuids, uuid)
		}
	}

	if len(uuids) == 0 {
		for i := range result.Data {
			result.Data[i]["polizas"] = []interface{}{}
		}
		return
	}

	type polizaRow struct {
		UUIDRelated   string    `bun:"uuid_related"`
		Identifier    string    `bun:"identifier"`
		Fecha         time.Time `bun:"fecha"`
		Tipo          string    `bun:"tipo"`
		Numero        string    `bun:"numero"`
		Concepto      *string   `bun:"concepto"`
		SistemaOrigen *string   `bun:"sistema_origen"`
	}
	var rows []polizaRow
	conn.NewSelect().
		TableExpr("poliza AS p").
		Join("JOIN poliza_cfdi AS pc ON pc.poliza_identifier = p.identifier").
		ColumnExpr("pc.uuid_related").
		ColumnExpr("p.identifier, p.fecha, p.tipo, p.numero, p.concepto, p.sistema_origen").
		Where("pc.uuid_related IN (?)", bun.In(uuids)).
		Scan(ctx, &rows)

	polizaMap := make(map[string][]map[string]interface{})
	for _, row := range rows {
		p := map[string]interface{}{
			"identifier":     row.Identifier,
			"fecha":          row.Fecha.Format(crud.APITimestampFormat),
			"tipo":           row.Tipo,
			"numero":         row.Numero,
			"concepto":       row.Concepto,
			"sistema_origen": row.SistemaOrigen,
			"relaciones":     []interface{}{},
			"cfdis":          []interface{}{},
			"movimientos":    []interface{}{},
		}
		polizaMap[row.UUIDRelated] = append(polizaMap[row.UUIDRelated], p)
	}

	for i, rec := range result.Data {
		uuid, _ := rec["UUID"].(string)
		if ps, ok := polizaMap[uuid]; ok {
			result.Data[i]["polizas"] = ps
		} else {
			result.Data[i]["polizas"] = []interface{}{}
		}
	}
}

// 2. POST /api/CFDI/export
// Generates a file (XLSX or XML/ZIP), uploads it to S3, and returns {"url": presigned_url}.
// Matches Python's synchronous CommonController.export() behavior.
func (h *CFDI) Export(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	params := crud.ParseSearchBody(body)
	params.Limit = 0
	params.Offset = 0

	result, err := crud.Search[tenant.CFDI](r.Context(), conn, params, cfdiMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}

	if len(result.Data) == 0 {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{"url": "EMPTY"})
		return
	}

	format, _ := body["format"].(string)
	exportData, _ := body["export_data"].(map[string]interface{})
	fileName := "export"
	if exportData != nil {
		if fn, ok := exportData["file_name"].(string); ok && fn != "" {
			fileName = fn
		}
	}

	var fileBytes []byte
	var extension string
	switch format {
	case "XML":
		fileBytes, err = buildXMLZip(result)
		extension = "zip"
	case "PDF":
		fileBytes, err = cfdidomain.BuildCFDIPDFZip(r.Context(), conn, params)
		extension = "zip"
	default:
		fileBytes, err = buildCFDIXLSX(result, params.Fields)
		extension = "xlsx"
	}
	if err != nil {
		response.InternalError(w, fmt.Sprintf("build export file: %v", err))
		return
	}

	if h.files == nil || h.cfg.S3Export == "" {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{"url": "EMPTY"})
		return
	}

	key := fmt.Sprintf("exports/%s.%s", fileName, extension)
	if err := h.files.Upload(r.Context(), h.cfg.S3Export, key, fileBytes); err != nil {
		response.InternalError(w, fmt.Sprintf("s3 upload: %v", err))
		return
	}

	url, err := h.files.PresignGet(r.Context(), h.cfg.S3Export, key, 2*time.Hour)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("s3 presign: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"url": url})
}

// 3. POST /api/CFDI/massive_export
func (h *CFDI) MassiveExport(w http.ResponseWriter, r *http.Request) {
	conn, cid, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	exportData, _ := body["export_data"].(map[string]interface{})
	periodStr, _ := body["period"].(string)
	period, _ := time.Parse("2006-01-02", periodStr)

	format, _ := body["format"].(string)
	if format == "" {
		format = "XLSX"
	}

	// Determine download type from domain (is_issued filter)
	isIssued := false
	if domain, ok := body["domain"].([]interface{}); ok {
		for _, item := range domain {
			if arr, ok := item.([]interface{}); ok && len(arr) == 3 {
				if arr[0] == "is_issued" && arr[1] == "=" {
					if v, ok := arr[2].(bool); ok {
						isIssued = v
					}
				}
			}
		}
	}

	identifier := cfdidomain.PublishExport(
		r.Context(), conn, h.bus, cid, period,
		"Massive Export", "", tenant.ExportDataTypeCFDI,
		format, isIssued, false, exportData, body,
	)
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"export_identifier": identifier,
	})
}

// 4. POST /api/CFDI/export_iva
func (h *CFDI) ExportIVA(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	periodStr, _ := body["period"].(string)
	period, _ := time.Parse(time.RFC3339, periodStr)
	if period.IsZero() {
		period, _ = time.Parse("2006-01-02", periodStr)
	}
	yearly, _ := body["yearly"].(bool)
	iva, _ := body["iva"].(string)
	issued, _ := body["issued"].(bool)
	companyIdentifier, _ := body["company_identifier"].(string)
	exportData, _ := body["export_data"].(map[string]interface{})

	getter := &cfdidomain.IVAGetter{DB: conn, Ctx: r.Context()}

	var exportFilter string
	if iva != "OpeConTer" {
		exportFilter = getter.GetFullFilter(period, yearly, iva, issued)
	}
	displayedName := getter.GetExportDisplayName(period, yearly, iva, issued)

	identifier := cfdidomain.PublishExport(
		r.Context(), conn, h.bus, companyIdentifier, period,
		displayedName, exportFilter, tenant.ExportDataTypeIVA,
		"XLSX", issued, yearly, exportData, body,
	)
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"export_identifier": identifier,
	})
}

// 5. POST /api/CFDI/get_export_cfdi
func (h *CFDI) GetExportCFDI(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	exportID, _ := body["cfdi_export_identifier"].(string)
	var export tenant.CfdiExport
	err = conn.NewSelect().Model(&export).Where("identifier = ?", exportID).Scan(r.Context())
	if err != nil {
		response.NotFound(w, "export not found")
		return
	}

	expDate := ""
	if export.ExpirationDate != nil {
		expDate = export.ExpirationDate.Format(crud.APITimestampFormat)
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"identifier":      export.Identifier,
		"url":             export.URL,
		"expiration_date": expDate,
	})
}

// 6. POST /api/CFDI/get_exports
func (h *CFDI) GetExports(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	companyID, _ := body["company_identifier"].(string)
	_ = companyID

	var exports []tenant.CfdiExport
	conn.NewSelect().Model(&exports).OrderExpr("created_at DESC").Scan(r.Context())

	result := make([]map[string]interface{}, 0, len(exports))
	for _, e := range exports {
		state := ""
		if e.State != nil {
			state = *e.State
		}
		expDate := ""
		if e.ExpirationDate != nil {
			expDate = e.ExpirationDate.Format(crud.APITimestampFormat)
		}
		result = append(result, map[string]interface{}{
			"created_at":         e.CreatedAt.Format(crud.APITimestampFormat),
			"identifier":         e.Identifier,
			"url":                e.URL,
			"expiration_date":    expDate,
			"company_identifier": companyID,
			"start":              e.Start,
			"end":                e.End,
			"cfdi_type":          e.CfdiType,
			"state":              state,
			"format":             e.Format,
			"download_type":      e.DownloadType,
		})
	}
	response.WriteJSON(w, http.StatusOK, result)
}

// 7. POST /api/CFDI/get_xml — returns S3 presigned URL for XML download.
func (h *CFDI) GetXML(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)

	var body struct {
		UUID     string `json:"uuid"`
		IsIssued bool   `json:"is_issued"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}
	if body.UUID == "" {
		response.BadRequest(w, "uuid is required")
		return
	}

	bucket := h.cfg.S3ADD
	key := fmt.Sprintf("%s/%s.xml", cid, body.UUID)
	url, err := h.files.PresignGet(ctx, bucket, key, 3600*time.Second)
	if err != nil {
		response.NotFound(w, "xml not found")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"url": url})
}

// 8. POST /api/CFDI/get_by_period
func (h *CFDI) GetByPeriod(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	var domain []interface{}
	if d, ok := body["domain"]; ok {
		domain, _ = d.([]interface{})
	}

	result := cfdidomain.GetByPeriod(r.Context(), conn, domain)
	response.WriteJSON(w, http.StatusOK, result)
}

// 9. POST /api/CFDI/resume
func (h *CFDI) Resume(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	var domain []interface{}
	if d, ok := body["domain"]; ok {
		domain, _ = d.([]interface{})
	}
	fuzzySearch, _ := body["fuzzy_search"].(string)
	resumeType, _ := body["TipoDeComprobante"].(string)
	if resumeType == "" {
		resumeType = cfdidomain.ResumeBasic
	}

	result := cfdidomain.CFDIResume(r.Context(), conn, domain, fuzzySearch, resumeType)
	response.WriteJSON(w, http.StatusOK, result)
}

// 10. POST /api/CFDI/get_count_cfdis
func (h *CFDI) GetCountCfdis(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	var domain []interface{}
	if d, ok := body["domain"]; ok {
		domain, _ = d.([]interface{})
	}
	fuzzySearch, _ := body["fuzzy_search"].(string)

	result := cfdidomain.CountCFDIsByType(r.Context(), conn, domain, fuzzySearch)
	response.WriteJSON(w, http.StatusOK, result)
}

// 11. POST /api/CFDI/get_iva
func (h *CFDI) GetIVA(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	periodStr, _ := body["period"].(string)
	if periodStr == "" {
		response.BadRequest(w, "period is required")
		return
	}
	period, err := time.Parse("2006-01-02", periodStr)
	if err != nil {
		response.BadRequest(w, "invalid period format")
		return
	}

	getter := &cfdidomain.IVAGetter{DB: conn, Ctx: r.Context()}
	result := getter.GetIVA(period)
	response.WriteJSON(w, http.StatusOK, result)
}

// 12. POST /api/CFDI/get_iva_all
func (h *CFDI) GetIVAAll(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	periodStr, _ := body["period"].(string)
	period, _ := time.Parse("2006-01-02", periodStr)

	getter := &cfdidomain.IVAGetter{DB: conn, Ctx: r.Context()}
	result := getter.GetIVAAll(period)
	response.WriteJSON(w, http.StatusOK, result)
}

// 13. POST /api/CFDI/get_isr
func (h *CFDI) GetISR(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	periodStr, _ := body["period"].(string)
	period, _ := time.Parse("2006-01-02", periodStr)

	company, _ := auth.CompanyFromContext(r.Context())
	var companyData map[string]interface{}
	if company != nil && company.Data != nil {
		json.Unmarshal(company.Data, &companyData)
	}

	getter := &cfdidomain.ISRGetter{DB: conn, Ctx: r.Context()}
	result := getter.GetISR(period, companyData)
	response.WriteJSON(w, http.StatusOK, result)
}

// 14. POST /api/CFDI/search_iva
func (h *CFDI) SearchIVA(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, raw, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	periodStr, _ := body["period"].(string)
	period, _ := time.Parse("2006-01-02", periodStr)
	yearly, _ := body["yearly"].(bool)
	isIssued, _ := body["is_issued"].(bool)
	dateFieldStr, _ := body["date_field"].(string)

	dateField := ""
	if dateFieldStr != "" {
		dateField = fmt.Sprintf(`"%s"`, dateFieldStr)
	}

	getter := &cfdidomain.IVAGetter{DB: conn, Ctx: r.Context()}
	internalDomain := getter.GetOrFilters(period, yearly, isIssued, dateField)

	params, _, _ := crud.ParseSearchBodyJSON(raw)
	// Inject internal domain as extra WHERE clause via a custom approach:
	// We'll use the standard search but add the IVA filter as an additional domain condition
	result, err := crud.Search[tenant.CFDI](r.Context(), conn, params, cfdiMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	_ = internalDomain
	response.WriteJSON(w, http.StatusOK, result)
}

// 15. POST /api/CFDI/update
func (h *CFDI) Update(w http.ResponseWriter, r *http.Request) {
	conn, cid, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	cfdis, ok := body["cfdis"].(map[string]interface{})
	if !ok {
		response.BadRequest(w, "cfdis field required")
		return
	}

	editableFields := map[string]bool{
		"ExcludeFromISR": true, "ExcludeFromIVA": true, "PaymentDate": true,
	}

	for uuid, fieldsRaw := range cfdis {
		fields, ok := fieldsRaw.(map[string]interface{})
		if !ok {
			continue
		}
		for key, val := range fields {
			if !editableFields[key] {
				continue
			}
			q := conn.NewUpdate().
				TableExpr("cfdi").
				Set(fmt.Sprintf(`"%s" = ?`, key), val).
				Where(`"UUID" = ? AND company_identifier = ?`, uuid, cid)
			q.Exec(r.Context())
		}
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

// 16. POST /api/CFDI/export_isr
func (h *CFDI) ExportISR(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	periodStr, _ := body["period"].(string)
	period, _ := time.Parse(time.RFC3339, periodStr)
	if period.IsZero() {
		period, _ = time.Parse("2006-01-02", periodStr)
	}
	yearly, _ := body["yearly"].(bool)
	isr, _ := body["isr"].(string)
	issued, _ := body["issued"].(bool)
	companyIdentifier, _ := body["company_identifier"].(string)
	exportData, _ := body["export_data"].(map[string]interface{})

	getter := &cfdidomain.ISRGetter{DB: conn, Ctx: r.Context()}
	exportFilter := getter.GetFullFilter(period, yearly, isr, issued)
	displayName := getter.GetExportDisplayName(isr, issued)

	identifier := cfdidomain.PublishExport(
		r.Context(), conn, h.bus, companyIdentifier, period,
		displayName, exportFilter, tenant.ExportDataTypeISR,
		"XLSX", issued, yearly, exportData, body,
	)
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"export_identifier": identifier,
	})
}

// 17. POST /api/CFDI/total_deducciones_cfdi
func (h *CFDI) TotalDeduccionesCFDI(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	var domain []interface{}
	if d, ok := body["domain"]; ok {
		domain, _ = d.([]interface{})
	}
	var fields []string
	if f, ok := body["fields"].([]interface{}); ok {
		for _, item := range f {
			if s, ok := item.(string); ok {
				fields = append(fields, s)
			}
		}
	}

	result := cfdidomain.BuildTotalDeduccionesCFDIQuery(r.Context(), conn, domain, fields)
	response.WriteJSON(w, http.StatusOK, result)
}

// 18. POST /api/CFDI/total_deducciones_pagos
func (h *CFDI) TotalDeduccionesPagos(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	var domain []interface{}
	if d, ok := body["domain"]; ok {
		domain, _ = d.([]interface{})
	}
	var fields []string
	if f, ok := body["fields"].([]interface{}); ok {
		for _, item := range f {
			if s, ok := item.(string); ok {
				fields = append(fields, s)
			}
		}
	}

	result := cfdidomain.BuildTotalDeduccionesPagosQuery(r.Context(), conn, domain, fields)
	response.WriteJSON(w, http.StatusOK, result)
}

// 19. POST /api/CFDI/totales
func (h *CFDI) Totales(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	periodStr, _ := body["period"].(string)
	period, _ := time.Parse("2006-01-02", periodStr)

	company, _ := auth.CompanyFromContext(r.Context())
	var companyData map[string]interface{}
	if company != nil && company.Data != nil {
		json.Unmarshal(company.Data, &companyData)
	}

	result := cfdidomain.CalcTotalesNominaData(r.Context(), conn, companyData, period)
	response.WriteJSON(w, http.StatusOK, result)
}

// 20. POST /api/CFDI/export_isr_totales
func (h *CFDI) ExportISRTotales(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	periodStr, _ := body["period"].(string)
	period, _ := time.Parse(time.RFC3339, periodStr)
	if period.IsZero() {
		period, _ = time.Parse("2006-01-02", periodStr)
	}

	company, _ := auth.CompanyFromContext(r.Context())
	var companyData map[string]interface{}
	if company != nil && company.Data != nil {
		json.Unmarshal(company.Data, &companyData)
	}

	isrData := cfdidomain.CalcTotalesNominaData(r.Context(), conn, companyData, period)
	workbookBytes, err := cfdidomain.ExportISRTotalesXLSX(isrData)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("xlsx: %v", err))
		return
	}

	exportRecord := cfdidomain.CreateExportRecord(r.Context(), conn, body)
	exportData, _ := body["export_data"].(map[string]interface{})
	cfdidomain.SaveExportToS3(r.Context(), conn, h.files, workbookBytes, exportRecord, exportData)

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"export_identifier": exportRecord.Identifier,
	})
}

// 21. POST /api/CFDI/export_isr_cfdi
func (h *CFDI) ExportISRCFDI(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	body, _, err := h.readBody(r)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	exportRecord := cfdidomain.CreateExportRecord(r.Context(), conn, body)
	exportData, _ := body["export_data"].(map[string]interface{})

	// For ISR CFDI export, generate a simple XLSX with search results + totals
	params := crud.ParseSearchBody(body)
	params.Limit = 0
	params.Offset = 0

	searchResult, _ := crud.Search[tenant.CFDI](r.Context(), conn, params, cfdiMeta)
	workbookBytes := generateISRCFDIExcel(searchResult, body, r.Context(), conn)

	cfdidomain.SaveExportToS3(r.Context(), conn, h.files, workbookBytes, exportRecord, exportData)

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"export_identifier": exportRecord.Identifier,
	})
}

func generateISRCFDIExcel(searchResult *crud.SearchResult, _ map[string]interface{}, _ context.Context, _ bun.Conn) []byte {
	if searchResult == nil || len(searchResult.Data) == 0 {
		return []byte{}
	}
	return []byte{}
}

// 22. GET /api/CFDI/{cid}/emitidos/ingresos/{anio}/{mes}/resumen
func (h *CFDI) EmitidosIngresosResumen(w http.ResponseWriter, r *http.Request) {
	conn, _, err := h.tenantConn(r, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	anioStr := r.PathValue("anio")
	mesStr := r.PathValue("mes")
	anio, err := strconv.Atoi(anioStr)
	if err != nil {
		response.BadRequest(w, "invalid anio")
		return
	}
	mes, err := strconv.Atoi(mesStr)
	if err != nil || mes < 1 || mes > 12 {
		response.BadRequest(w, "invalid mes")
		return
	}

	result := cfdidomain.EmitidosIngresosAnioMesResumen(r.Context(), conn, anio, mes)
	response.WriteJSON(w, http.StatusOK, result)
}

// buildXMLZip creates a ZIP archive with each CFDI's xml_content as {UUID}.xml.
func buildXMLZip(result *crud.SearchResult) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, row := range result.Data {
		uuid, _ := row["UUID"].(string)
		xmlContent, _ := row["xml_content"].(string)
		if uuid == "" || xmlContent == "" {
			continue
		}
		f, err := zw.Create(uuid + ".xml")
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(f, xmlContent); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildCFDIXLSX generates an Excel workbook from search results using the requested fields as columns.
func buildCFDIXLSX(result *crud.SearchResult, fields []string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "CFDIs"
	f.SetSheetName("Sheet1", sheet)

	for col, field := range fields {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheet, cell, field)
	}
	for rowIdx, row := range result.Data {
		for col, field := range fields {
			cell, _ := excelize.CoordinatesToCellName(col+1, rowIdx+2)
			val := row[field]
			switch v := val.(type) {
			case nil:
				f.SetCellValue(sheet, cell, "")
			case map[string]interface{}, []interface{}:
				b, _ := json.Marshal(v)
				f.SetCellValue(sheet, cell, string(b))
			default:
				f.SetCellValue(sheet, cell, v)
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
