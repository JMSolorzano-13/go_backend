package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/filter"
	"github.com/siigofiscal/go_backend/internal/response"
)

type CfdiExcluded struct {
	cfg      *config.Config
	database *db.Database
}

func NewCfdiExcluded(cfg *config.Config, database *db.Database) *CfdiExcluded {
	return &CfdiExcluded{cfg: cfg, database: database}
}

func (h *CfdiExcluded) Search(w http.ResponseWriter, r *http.Request) {
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
	params, body, err := crud.ParseSearchBodyJSON(raw)
	if err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	domain := filter.StripCompanyIdentifier(params.Domain)

	result, err := searchExcludedCFDIs(ctx, conn, params, domain, body)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

func searchExcludedCFDIs(ctx context.Context, conn bun.Conn, params crud.SearchParams, domain []interface{}, body map[string]interface{}) (*crud.SearchResult, error) {
	cfdiPart := `SELECT
		c.company_identifier,
		c."Estatus",
		c.is_issued,
		c."Version",
		c."ExcludeFromIVA",
		c."Fecha",
		c."PaymentDate",
		c."UUID",
		c."Serie",
		c."Folio",
		c."RfcEmisor",
		c."NombreEmisor",
		c."TipoDeComprobante",
		c."UsoCFDIReceptor",
		c."FormaPago",
		c."MetodoPago",
		c."BaseIVA16",
		c."BaseIVA8",
		c."BaseIVA0",
		c."BaseIVAExento",
		c."IVATrasladado16",
		c."IVATrasladado8",
		c."TrasladosIVAMXN" AS "TrasladosIVA",
		c."RetencionesIVAMXN" AS "RetencionesIVA",
		c."TotalMXN" AS "Total",
		NULL::uuid AS "DR_UUID",
		NULL::uuid AS "DR_Identifier"
	FROM cfdi c
	WHERE (c."MetodoPago" = 'PUE' OR c."MetodoPago" IS NULL)`

	doctoPart := `SELECT
		dr.company_identifier,
		(cfdi_rel."Estatus" AND dr."Estatus") AS "Estatus",
		cfdi_rel.is_issued,
		cfdi_rel."Version",
		dr."ExcludeFromIVA",
		cfdi_rel."Fecha",
		p."FechaPago" AS "PaymentDate",
		cfdi_rel."UUID",
		dr."Serie",
		dr."Folio",
		cfdi_rel."RfcEmisor",
		cfdi_rel."NombreEmisor",
		cfdi_rel."TipoDeComprobante",
		cfdi_rel."UsoCFDIReceptor",
		p."FormaDePagoP" AS "FormaPago",
		cfdi_rel."MetodoPago",
		dr."BaseIVA16",
		dr."BaseIVA8",
		dr."BaseIVA0",
		dr."BaseIVAExento",
		dr."IVATrasladado16",
		dr."IVATrasladado8",
		dr."TrasladosIVAMXN" AS "TrasladosIVA",
		dr."RetencionesIVAMXN" AS "RetencionesIVA",
		dr."ImpPagadoMXN" AS "Total",
		dr."UUID" AS "DR_UUID",
		dr.identifier AS "DR_Identifier"
	FROM payment_relation dr
	JOIN cfdi cfdi_rel ON dr."UUID_related" = cfdi_rel."UUID"
	JOIN payment p ON dr.payment_identifier = p.identifier`

	unionSQL := fmt.Sprintf("(%s) UNION ALL (%s)", cfdiPart, doctoPart)

	countQ := conn.NewSelect().TableExpr("(?) AS sub", bun.Safe(unionSQL))
	applyExcludedDomainFilters(countQ, domain)
	totalRecords, err := countQ.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	dataQ := conn.NewSelect().TableExpr("(?) AS sub", bun.Safe(unionSQL)).ColumnExpr("sub.*")
	applyExcludedDomainFilters(dataQ, domain)

	orderBy := params.OrderBy
	if orderBy == "" {
		orderBy = `"PaymentDate" DESC`
	}
	dataQ = filter.ApplyOrderBy(dataQ, orderBy)

	if params.Limit > 0 {
		offset := params.Offset
		if offset < 0 {
			offset = 0
		}
		dataQ = dataQ.Offset(offset * params.Limit).Limit(params.Limit)
	}

	rows, err := dataQ.Rows(ctx)
	if err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var data []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		m := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			m[col] = normalizeDBValue(values[i])
		}
		data = append(data, m)
	}
	if data == nil {
		data = []map[string]interface{}{}
	}

	nextPage := false
	if params.Limit > 0 && totalRecords > 0 {
		nextPage = (params.Offset+1)*params.Limit < totalRecords
	}

	return &crud.SearchResult{
		Data:         data,
		NextPage:     nextPage,
		TotalRecords: totalRecords,
	}, nil
}

func applyExcludedDomainFilters(q *bun.SelectQuery, domain []interface{}) {
	for _, d := range domain {
		cond, ok := d.([]interface{})
		if !ok || len(cond) != 3 {
			continue
		}
		field, _ := cond[0].(string)
		op, _ := cond[1].(string)
		value := cond[2]

		col := fmt.Sprintf(`sub."%s"`, field)
		switch op {
		case "=":
			q.Where(col+" = ?", value)
		case "!=":
			q.Where(col+" != ?", value)
		case ">":
			q.Where(col+" > ?", value)
		case ">=":
			q.Where(col+" >= ?", value)
		case "<":
			q.Where(col+" < ?", value)
		case "<=":
			q.Where(col+" <= ?", value)
		case "in":
			if arr, ok := value.([]interface{}); ok {
				q.Where(col+" IN (?)", bun.In(arr))
			}
		case "not in":
			if arr, ok := value.([]interface{}); ok {
				q.Where(col+" NOT IN (?)", bun.In(arr))
			}
		case "ilike":
			if s, ok := value.(string); ok {
				q.Where(col+" ILIKE ?", "%"+s+"%")
			}
		}
	}
}

func normalizeDBValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []byte:
		var j interface{}
		if err := json.Unmarshal(val, &j); err == nil {
			return j
		}
		return string(val)
	default:
		return val
	}
}
