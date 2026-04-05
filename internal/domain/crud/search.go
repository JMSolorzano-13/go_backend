package crud

import (
	"context"
	"fmt"
	"strings"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/domain/filter"
)

// Search executes a generic paginated search matching Python's dependencies/common.py:search.
//
// It applies domain filters, active filter, fuzzy search, ordering, and pagination,
// then returns a SearchResult with serialized data, next_page flag, and total count.
func Search[T any](ctx context.Context, idb bun.IDB, params SearchParams, meta ModelMeta) (*SearchResult, error) {
	domain := filter.StripCompanyIdentifier(params.Domain)
	domain = stripUnsupportedFilters(domain)

	if meta.TableAlias != "" {
		domain = qualifyDomainFields(domain, meta.TableAlias)
		meta = qualifyMeta(meta)
	}

	// Count query (no ordering or pagination). Must apply the same JOINs as the data
	// query when the domain references related tables (e.g. user.email on permission).
	countQ := idb.NewSelect().Model((*T)(nil))
	for _, rel := range meta.Relations {
		countQ = countQ.Relation(rel)
	}
	countQ, err := applyFilters(countQ, domain, params, meta)
	if err != nil {
		return nil, fmt.Errorf("apply filters for count: %w", err)
	}
	totalRecords, err := countQ.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	// Data query
	var records []T
	dataQ := idb.NewSelect().Model(&records)
	for _, rel := range meta.Relations {
		dataQ = dataQ.Relation(rel)
	}
	dataQ, err = applyFilters(dataQ, domain, params, meta)
	if err != nil {
		return nil, fmt.Errorf("apply filters for data: %w", err)
	}

	orderBy := params.OrderBy
	if orderBy == "" {
		orderBy = meta.DefaultOrderBy
	}
	if orderBy != "" {
		dataQ = filter.ApplyOrderBy(dataQ, orderBy)
	}

	if params.Limit > 0 {
		offset := params.Offset
		if offset < 0 {
			offset = 0
		}
		dataQ = dataQ.Offset(offset * params.Limit).Limit(params.Limit)
	}

	if err := dataQ.Scan(ctx); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	data := Serialize(records)

	nextPage := false
	if params.Limit > 0 && totalRecords > 0 {
		nextPage = (params.Offset+1)*params.Limit < totalRecords
	}

	return &SearchResult{
		Data:         data,
		NextPage:     nextPage,
		TotalRecords: totalRecords,
	}, nil
}

// SearchWithQuery is a lower-level variant that accepts a pre-built query.
// Useful when the handler needs custom JOINs, column selection, or subqueries
// beyond what the generic Search provides.
func SearchWithQuery(ctx context.Context, q *bun.SelectQuery, countQ *bun.SelectQuery, params SearchParams) (*SearchResult, []map[string]interface{}, error) {
	totalRecords, err := countQ.Count(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("count: %w", err)
	}

	if params.OrderBy != "" {
		q = filter.ApplyOrderBy(q, params.OrderBy)
	}

	if params.Limit > 0 {
		offset := params.Offset
		if offset < 0 {
			offset = 0
		}
		q = q.Offset(offset * params.Limit).Limit(params.Limit)
	}

	rows, err := q.Rows(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("rows: %w", err)
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
			return nil, nil, fmt.Errorf("scan row: %w", err)
		}
		m := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			m[col] = normalizeValue(values[i])
		}
		data = append(data, m)
	}

	nextPage := false
	if params.Limit > 0 && totalRecords > 0 {
		nextPage = (params.Offset+1)*params.Limit < totalRecords
	}

	return &SearchResult{
		Data:         data,
		NextPage:     nextPage,
		TotalRecords: totalRecords,
	}, data, nil
}

func applyFilters(q *bun.SelectQuery, domain []interface{}, params SearchParams, meta ModelMeta) (*bun.SelectQuery, error) {
	var err error
	q, err = filter.ApplyDomain(q, domain)
	if err != nil {
		return nil, err
	}

	q = applyActiveFilter(q, params.Active, meta.ActiveColumn)

	if params.FuzzySearch != "" && len(meta.FuzzyFields) > 0 {
		q = filter.ApplyFuzzySearch(q, params.FuzzySearch, meta.FuzzyFields)
	}

	return q, nil
}

func applyActiveFilter(q *bun.SelectQuery, active *bool, column string) *bun.SelectQuery {
	if column == "" {
		return q
	}

	val := true
	if active != nil {
		val = *active
	}

	col := filter.QuoteIdent(column)
	if val {
		q = q.Where(col+" = ? OR "+col+" IS NULL", true)
	} else {
		q = q.Where(col+" = ?", false)
	}
	return q
}

// qualifyMeta returns a copy of meta with column references prefixed by the table alias.
func qualifyMeta(meta ModelMeta) ModelMeta {
	alias := meta.TableAlias
	if meta.ActiveColumn != "" && !strings.Contains(meta.ActiveColumn, ".") {
		meta.ActiveColumn = alias + "." + meta.ActiveColumn
	}
	if meta.DefaultOrderBy != "" {
		meta.DefaultOrderBy = qualifyOrderBy(meta.DefaultOrderBy, alias)
	}
	if len(meta.FuzzyFields) > 0 {
		qualified := make([]string, len(meta.FuzzyFields))
		for i, f := range meta.FuzzyFields {
			if !strings.Contains(f, ".") {
				qualified[i] = alias + "." + f
			} else {
				qualified[i] = f
			}
		}
		meta.FuzzyFields = qualified
	}
	return meta
}

// stripUnsupportedFilters removes filter conditions that are not supported in Go backend.
// Currently removes:
// - "balance" filter: This is a computed property in Python (Total - SUM(payment_relation.ImpPagado))
//   that doesn't exist as a database column and would require complex subquery implementation.
func stripUnsupportedFilters(domain []interface{}) []interface{} {
	result := make([]interface{}, 0, len(domain))
	for _, item := range domain {
		sl, ok := item.([]interface{})
		if !ok {
			result = append(result, item)
			continue
		}
		if len(sl) < 3 {
			result = append(result, item)
			continue
		}
		field, ok := sl[0].(string)
		if !ok {
			result = append(result, item)
			continue
		}
		if strings.EqualFold(field, "balance") {
			continue
		}
		result = append(result, item)
	}
	return result
}

func qualifyOrderBy(orderBy, alias string) string {
	var parts []string
	for _, part := range strings.Split(orderBy, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Fields(part)
		col := fields[0]
		if !strings.Contains(col, ".") {
			fields[0] = alias + "." + col
		}
		parts = append(parts, strings.Join(fields, " "))
	}
	return strings.Join(parts, ", ")
}

// qualifyDomainFields recursively prefixes unqualified field names with the table alias.
func qualifyDomainFields(domain []interface{}, alias string) []interface{} {
	if len(domain) == 0 {
		return domain
	}
	result := make([]interface{}, len(domain))
	for i, item := range domain {
		sl, ok := toSlice(item)
		if !ok {
			result[i] = item
			continue
		}
		if isConditionLike(sl) {
			qualified := make([]interface{}, len(sl))
			copy(qualified, sl)
			if field, ok := sl[0].(string); ok && !strings.Contains(field, ".") {
				qualified[0] = alias + "." + field
			}
			result[i] = qualified
		} else {
			result[i] = qualifyDomainFields(sl, alias)
		}
	}
	return result
}

func isConditionLike(items []interface{}) bool {
	if len(items) != 3 {
		return false
	}
	_, fieldOk := items[0].(string)
	_, opOk := items[1].(string)
	return fieldOk && opOk
}

func toSlice(v interface{}) ([]interface{}, bool) {
	switch s := v.(type) {
	case []interface{}:
		return s, true
	case []string:
		r := make([]interface{}, len(s))
		for i, x := range s {
			r[i] = x
		}
		return r, true
	default:
		return nil, false
	}
}
