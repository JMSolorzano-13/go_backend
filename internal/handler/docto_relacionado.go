package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

type DoctoRelacionado struct {
	cfg      *config.Config
	database *db.Database
	files    port.FileStorage
}

func NewDoctoRelacionado(cfg *config.Config, database *db.Database, files port.FileStorage) *DoctoRelacionado {
	return &DoctoRelacionado{cfg: cfg, database: database, files: files}
}

var doctoRelacionadoMeta = crud.ModelMeta{
	DefaultOrderBy: `"Folio" ASC`,
	ActiveColumn:   "active",
}

func (h *DoctoRelacionado) Search(w http.ResponseWriter, r *http.Request) {
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

	result, err := crud.Search[tenant.DoctoRelacionado](ctx, conn, params, doctoRelacionadoMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	enrichDoctoRelacionadoNested(ctx, conn, cid, result)
	response.WriteJSON(w, http.StatusOK, result)
}

func (h *DoctoRelacionado) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	var body struct {
		CFDIs []map[string]interface{} `json:"cfdis"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	if len(body.CFDIs) == 0 {
		response.WriteJSON(w, http.StatusOK, map[string]string{"result": "ok"})
		return
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("begin tx: %v", err))
		return
	}
	defer tx.Rollback()

	for _, record := range body.CFDIs {
		identifier, ok := record["identifier"].(string)
		if !ok || identifier == "" {
			response.BadRequest(w, "each record must have an 'identifier'")
			return
		}

		q := tx.NewUpdate().Model((*tenant.DoctoRelacionado)(nil)).
			Where("identifier = ?", identifier).
			Where("company_identifier = ?", cid)

		for col, val := range record {
			if col == "identifier" || col == "company_identifier" {
				continue
			}
			if crud.RestrictedUpdateFields[col] {
				response.Forbidden(w, fmt.Sprintf("the field '%s' cannot be updated manually", col))
				return
			}
			q = q.Set(fmt.Sprintf(`"%s" = ?`, col), val)
		}

		if _, err := q.Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("update record %s: %v", identifier, err))
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.InternalError(w, fmt.Sprintf("commit: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func (h *DoctoRelacionado) ExportISRPagos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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

	period, _ := body["period"].(string)
	displayedName, _ := body["displayed_name"].(string)
	issued, _ := body["issued"].(bool)
	exportDataMap, _ := body["export_data"].(map[string]interface{})
	fileName := "isr_pagos_export"
	if exportDataMap != nil {
		if fn, ok := exportDataMap["file_name"].(string); ok {
			fileName = fn
		}
	}

	downloadType := "RECEIVED"
	if issued {
		downloadType = "ISSUED"
	}

	exportRecord := tenant.CfdiExport{
		Identifier:     crud.NewIdentifier(),
		Start:          strPtr(period),
		DisplayedName:  displayedName,
		ExportDataType: strPtr("ISR"),
		Format:         strPtr("XLSX"),
		DownloadType:   strPtr(downloadType),
		FileName:       fileName,
	}

	if _, err := conn.NewInsert().Model(&exportRecord).Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("create export record: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"export_identifier": exportRecord.Identifier,
	})
}
