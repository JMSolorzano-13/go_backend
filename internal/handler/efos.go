package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/filter"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

const satFileURL = "http://omawww.sat.gob.mx/cifras_sat/Documents/Listado_Completo_69-B.csv"

var stateMap = map[string]string{
	"Definitivo":          control.EFOSStateDefinitive,
	"Desvirtuado":         control.EFOSStateDistorted,
	"Presunto":            control.EFOSStateAlleged,
	"Sentencia Favorable": control.EFOSStateFavorableJudgment,
}

type EFOS struct {
	cfg      *config.Config
	database *db.Database
}

func NewEFOS(cfg *config.Config, database *db.Database) *EFOS {
	return &EFOS{cfg: cfg, database: database}
}

var efosMeta = crud.ModelMeta{
	DefaultOrderBy: "id ASC",
	FuzzyFields:    []string{"rfc", "name"},
}

func (h *EFOS) UpdateFromSAT(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}
	pool := database.Pool(false)

	resp, err := http.Get(satFileURL)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("download SAT file: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		response.InternalError(w, fmt.Sprintf("SAT file returned status %d", resp.StatusCode))
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("read SAT file: %v", err))
		return
	}

	content := tryDecode(bodyBytes)
	records, err := parseEFOSCSV(content)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("parse SAT CSV: %v", err))
		return
	}

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("begin tx: %v", err))
		return
	}
	defer tx.Rollback()

	if _, err := tx.NewDelete().Model((*control.EFOS)(nil)).Where("1=1").Exec(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("delete existing EFOS: %v", err))
		return
	}

	if len(records) > 0 {
		const batchSize = 1000
		for i := 0; i < len(records); i += batchSize {
			end := i + batchSize
			if end > len(records) {
				end = len(records)
			}
			batch := records[i:end]
			if _, err := tx.NewInsert().Model(&batch).Exec(ctx); err != nil {
				response.InternalError(w, fmt.Sprintf("insert EFOS batch: %v", err))
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		response.InternalError(w, fmt.Sprintf("commit: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"state":   "success",
		"created": len(records),
	})
}

func (h *EFOS) Search(w http.ResponseWriter, r *http.Request) {
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

	result, err := crud.Search[control.EFOS](ctx, conn, params, efosMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

func (h *EFOS) Resume(w http.ResponseWriter, r *http.Request) {
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

	states := []string{
		control.EFOSStateDefinitive,
		control.EFOSStateDistorted,
		control.EFOSStateAlleged,
		control.EFOSStateFavorableJudgment,
	}

	result := make(map[string]int, len(states))
	for _, state := range states {
		count, err := countEFOSByState(ctx, conn, params, state)
		if err != nil {
			response.InternalError(w, fmt.Sprintf("count %s: %v", state, err))
			return
		}
		result[state] = count
	}

	response.WriteJSON(w, http.StatusOK, result)
}

func countEFOSByState(ctx context.Context, idb bun.IDB, params crud.SearchParams, state string) (int, error) {
	domain := filter.StripCompanyIdentifier(params.Domain)

	q := idb.NewSelect().Model((*control.EFOS)(nil)).Where("state = ?", state)

	var err error
	q, err = filter.ApplyDomain(q, domain)
	if err != nil {
		return 0, err
	}

	if params.FuzzySearch != "" && len(efosMeta.FuzzyFields) > 0 {
		q = filter.ApplyFuzzySearch(q, params.FuzzySearch, efosMeta.FuzzyFields)
	}

	return q.Count(ctx)
}

func tryDecode(data []byte) string {
	encodings := []string{"utf-8", "windows-1252"}
	for _, enc := range encodings {
		switch enc {
		case "utf-8":
			s := string(data)
			if !strings.Contains(s, "\ufffd") {
				return s
			}
		case "windows-1252":
			return string(data)
		}
	}
	return string(data)
}

func parseEFOSCSV(content string) ([]control.EFOS, error) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return nil, fmt.Errorf("CSV too short: %d lines", len(lines))
	}
	lines = lines[2:]

	reader := csv.NewReader(strings.NewReader(strings.Join(lines, "\n")))
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}

	var records []control.EFOS
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		getVal := func(csvCol string) string {
			if idx, ok := colIdx[csvCol]; ok && idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}

		noStr := getVal("No")
		if noStr == "" {
			continue
		}
		no, err := strconv.ParseInt(noStr, 10, 64)
		if err != nil {
			continue
		}

		stateStr := getVal("Situación del contribuyente")
		state, ok := stateMap[stateStr]
		if !ok {
			continue
		}

		rfc := getVal("RFC")
		name := getVal("Nombre del Contribuyente")
		if rfc == "" || name == "" {
			continue
		}

		efos := control.EFOS{
			Identifier:                       crud.NewIdentifier(),
			No:                               no,
			RFC:                              rfc,
			Name:                             name,
			State:                            state,
			SATPublishAllegedDate:            getVal("Publicación página SAT presuntos"),
			SATOfficeAlleged:                 optStr(getVal("Número y fecha de oficio global de presunción SAT")),
			DOFOfficeAlleged:                 optStr(getVal("Número y fecha de oficio global de presunción DOF")),
			DOFPublishAllegedDate:            optStr(getVal("Publicación DOF presuntos")),
			SATOfficeDistored:                optStr(getVal("Número y fecha de oficio global de contribuyentes que desvirtuaron SAT")),
			SATPublishDistoredDate:           optStr(getVal("Publicación página SAT desvirtuados")),
			DOFOfficeDistored:                optStr(getVal("Número y fecha de oficio global de contribuyentes que desvirtuaron DOF")),
			DOFPublishDistoredDate:           optStr(getVal("Publicación DOF desvirtuados")),
			SATOfficeDefinitive:              optStr(getVal("Número y fecha de oficio global de definitivos SAT")),
			SATPublishDefinitiveDate:         optStr(getVal("Publicación página SAT definitivos")),
			DOFOfficeDefinitive:              optStr(getVal("Número y fecha de oficio global de definitivos DOF")),
			DOFPublishDefinitiveDate:         optStr(getVal("Publicación DOF definitivos")),
			SATOfficeFavorableJudgement:      optStr(getVal("Número y fecha de oficio global de sentencia favorable SAT")),
			SATPublishFavorableJudgementDate: optStr(getVal("Publicación página SAT sentencia favorable")),
			DOFOfficeFavorableJudgement:      optStr(getVal("Número y fecha de oficio global de sentencia favorable DOF")),
			DOFPublishFavorableJudgementDate: optStr(getVal("Publicación DOF sentencia favorable")),
		}
		records = append(records, efos)
	}

	return records, nil
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
