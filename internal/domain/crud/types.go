package crud

import (
	"encoding/json"

	"github.com/siigofiscal/go_backend/internal/domain/filter"
)

// SearchParams holds the parsed parameters for a generic search request.
type SearchParams struct {
	Domain      []interface{}
	Fields      []string
	OrderBy     string
	Limit       int
	Offset      int
	Active      *bool
	FuzzySearch string
}

// SearchResult is the standard paginated response matching Python's
// {"data": [...], "next_page": bool, "total_records": int}
type SearchResult struct {
	Data         []map[string]interface{} `json:"data"`
	NextPage     bool                     `json:"next_page"`
	TotalRecords int                      `json:"total_records"`
}

// ModelMeta describes per-model CRUD behavior that the generic layer needs.
type ModelMeta struct {
	DefaultOrderBy string
	FuzzyFields    []string
	ActiveColumn   string   // e.g. "active"; empty means model has no active flag
	Relations      []string // bun relations to eagerly load (e.g. "Workspace")
	TableAlias     string   // bun table alias (e.g. "c"); qualifies columns to avoid ambiguity with JOINs
}

// ParseSearchBody extracts SearchParams from a raw JSON body map.
func ParseSearchBody(body map[string]interface{}) SearchParams {
	params := SearchParams{
		Limit: filter.DefaultPageSize,
	}

	if d, ok := body["domain"]; ok {
		if domain, ok := d.([]interface{}); ok {
			params.Domain = domain
		}
	}

	if f, ok := body["fields"]; ok {
		switch v := f.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					params.Fields = append(params.Fields, s)
				}
			}
		case []string:
			params.Fields = v
		}
	}

	if o, ok := body["order_by"]; ok {
		if s, ok := o.(string); ok {
			params.OrderBy = s
		}
	}

	if l, ok := body["limit"]; ok {
		switch v := l.(type) {
		case float64:
			params.Limit = int(v)
		case int:
			params.Limit = v
		case json.Number:
			if n, err := v.Int64(); err == nil {
				params.Limit = int(n)
			}
		}
	}

	if o, ok := body["offset"]; ok {
		switch v := o.(type) {
		case float64:
			params.Offset = int(v)
		case int:
			params.Offset = v
		case json.Number:
			if n, err := v.Int64(); err == nil {
				params.Offset = int(n)
			}
		}
	}

	if a, ok := body["active"]; ok {
		switch v := a.(type) {
		case bool:
			params.Active = &v
		}
	}

	if fs, ok := body["fuzzy_search"]; ok {
		if s, ok := fs.(string); ok {
			params.FuzzySearch = s
		}
	}

	return params
}

// ParseSearchBodyJSON decodes a JSON byte slice and returns SearchParams.
func ParseSearchBodyJSON(data []byte) (SearchParams, map[string]interface{}, error) {
	var body map[string]interface{}
	if err := json.Unmarshal(data, &body); err != nil {
		return SearchParams{Limit: filter.DefaultPageSize}, nil, err
	}
	return ParseSearchBody(body), body, nil
}
