package filter

import (
	"fmt"
	"strings"

	"github.com/uptrace/bun"
)

const DefaultPageSize = 80

// StripCompanyIdentifier removes company_identifier conditions from the domain
// since the auth middleware already handles it.
func StripCompanyIdentifier(domain []interface{}) []interface{} {
	result := make([]interface{}, 0, len(domain))
	for _, item := range domain {
		sl, ok := toSlice(item)
		if ok && len(sl) == 3 {
			if field, ok := sl[0].(string); ok && field == "company_identifier" {
				continue
			}
		}
		result = append(result, item)
	}
	return result
}

// GetCompanyIdentifier extracts company_identifier from the domain (first condition).
func GetCompanyIdentifier(domain []interface{}) string {
	if len(domain) == 0 {
		return ""
	}
	first, ok := toSlice(domain[0])
	if !ok || len(first) != 3 {
		return ""
	}
	field, _ := first[0].(string)
	op, _ := first[1].(string)
	if field == "company_identifier" && op == "=" {
		if v, ok := first[2].(string); ok {
			return v
		}
	}
	return ""
}

// ApplyDomain parses an Odoo-style domain filter array and applies WHERE clauses.
//
// Supported formats:
//   - Single condition: ["field", "op", value]
//   - AND conditions:   [["f1","=","v1"], ["f2","=","v2"]]
//   - OR conditions:    ["|", [["f1","=","v1"], ["f2","=","v2"]]]
func ApplyDomain(q *bun.SelectQuery, domain []interface{}) (*bun.SelectQuery, error) {
	if len(domain) == 0 {
		return q, nil
	}

	if isCondition(domain) {
		return applyCondition(q, domain)
	}

	if s, ok := domain[0].(string); ok && s == "|" {
		if len(domain) < 2 {
			return q, nil
		}
		orConds, ok := toSlice(domain[1])
		if !ok {
			return q, fmt.Errorf("OR group must contain a conditions array")
		}
		return applyOrGroup(q, orConds)
	}

	return applyAndConditions(q, domain)
}

func applyAndConditions(q *bun.SelectQuery, domain []interface{}) (*bun.SelectQuery, error) {
	for _, item := range domain {
		sl, ok := toSlice(item)
		if !ok {
			continue
		}
		if isCondition(sl) {
			var err error
			q, err = applyCondition(q, sl)
			if err != nil {
				return q, err
			}
		} else {
			var err error
			q, err = ApplyDomain(q, sl)
			if err != nil {
				return q, err
			}
		}
	}
	return q, nil
}

func applyOrGroup(q *bun.SelectQuery, conditions []interface{}) (*bun.SelectQuery, error) {
	var clauses []string
	var allArgs []interface{}

	for _, item := range conditions {
		sl, ok := toSlice(item)
		if !ok {
			continue
		}
		if isCondition(sl) {
			clause, args, err := buildConditionSQL(sl)
			if err != nil {
				return q, err
			}
			clauses = append(clauses, clause)
			allArgs = append(allArgs, args...)
		}
	}

	if len(clauses) > 0 {
		orExpr := "(" + strings.Join(clauses, " OR ") + ")"
		q = q.Where(orExpr, allArgs...)
	}
	return q, nil
}

func applyCondition(q *bun.SelectQuery, cond []interface{}) (*bun.SelectQuery, error) {
	clause, args, err := buildConditionSQL(cond)
	if err != nil {
		return q, err
	}
	q = q.Where(clause, args...)
	return q, nil
}

// virtualFieldSQL handles SQLAlchemy hybrid_property fields that have no real DB column.
// These are computed properties defined in the Python model that expand to SQL expressions.
func virtualFieldSQL(field string, value interface{}) (string, []interface{}, bool, error) {
	switch field {
	case "used_in_isr":
		// Python: or_(TipoDeComprobante == "P", and_(TipoDeComprobante == "I", MetodoPago == "PUE"))
		b, ok := value.(bool)
		if !ok {
			return "", nil, true, fmt.Errorf("used_in_isr requires a boolean value")
		}
		if b {
			sql := `("TipoDeComprobante" = ? OR ("TipoDeComprobante" = ? AND "MetodoPago" = ?))`
			return sql, []interface{}{"P", "I", "PUE"}, true, nil
		}
		sql := `NOT ("TipoDeComprobante" = ? OR ("TipoDeComprobante" = ? AND "MetodoPago" = ?))`
		return sql, []interface{}{"P", "I", "PUE"}, true, nil
	}
	return "", nil, false, nil
}

func buildConditionSQL(cond []interface{}) (string, []interface{}, error) {
	if len(cond) != 3 {
		return "", nil, fmt.Errorf("condition must have exactly 3 elements, got %d", len(cond))
	}
	field, ok := cond[0].(string)
	if !ok {
		return "", nil, fmt.Errorf("condition field must be a string")
	}
	op, ok := cond[1].(string)
	if !ok {
		return "", nil, fmt.Errorf("condition operator must be a string")
	}
	value := cond[2]

	if s, ok := value.(string); ok && s == "null" {
		value = nil
	}

	// Python parity: chalicelib/controllers/__init__.py:_get_filter_m2o — "efos" is a
	// many-to-one to public.efos (SQLAlchemy primaryjoin EFOS.rfc == CFDI.RfcEmisor).
	// Only value "any" and ops "=" / "!=" are accepted (same as Python).
	if field == "efos" {
		v, ok := value.(string)
		if !ok || v != "any" {
			return "", nil, fmt.Errorf(`efos filter only accepts value "any"`)
		}
		switch op {
		case "=":
			return `"RfcEmisor" IN (SELECT "rfc" FROM "public"."efos")`, nil, nil
		case "!=":
			return `"RfcEmisor" NOT IN (SELECT "rfc" FROM "public"."efos")`, nil, nil
		default:
			return "", nil, fmt.Errorf(`efos filter only supports "=" and "!=" operators`)
		}
	}

	// Python parity: SQLAlchemy backref EFOS.cfdis (to-many); frontend sends
	// ["cfdis.is_issued", "=", "false"] for "Operaciones con EFOS".
	// Requires tenant search_path so "cfdi" resolves to the company schema.
	if field == "cfdis.is_issued" {
		b, err := coerceToBool(value)
		if err != nil {
			return "", nil, fmt.Errorf("cfdis.is_issued: %w", err)
		}
		var want bool
		switch op {
		case "=":
			want = b
		case "!=":
			want = !b
		default:
			return "", nil, fmt.Errorf(`cfdis.is_issued only supports "=" and "!="`)
		}
		// Correlate with outer row: control.EFOS is `bun:"table:efos,alias:e"`. Referencing
		// "public"."efos" here breaks PostgreSQL (42P01) when the outer FROM uses alias "e" only.
		sql := `EXISTS (SELECT 1 FROM "cfdi" AS _cfdi_ic WHERE _cfdi_ic."RfcEmisor" = "e"."rfc" AND _cfdi_ic."is_issued" = ?)`
		return sql, []interface{}{want}, nil
	}

	// Check for virtual/computed fields before attempting column lookup
	if sql, args, handled, err := virtualFieldSQL(field, value); handled {
		return sql, args, err
	}

	col := QuoteIdent(field)

	switch op {
	case "=":
		if value == nil {
			return col + " IS NULL", nil, nil
		}
		return col + " = ?", []interface{}{value}, nil

	case "!=":
		if value == nil {
			return col + " IS NOT NULL", nil, nil
		}
		return col + " != ?", []interface{}{value}, nil

	case ">":
		return col + " > ?", []interface{}{value}, nil
	case ">=":
		return col + " >= ?", []interface{}{value}, nil
	case "<":
		return col + " < ?", []interface{}{value}, nil
	case "<=":
		return col + " <= ?", []interface{}{value}, nil

	case "in":
		vals := toInterfaceSlice(value)
		if len(vals) == 0 {
			return "FALSE", nil, nil
		}
		placeholders := make([]string, len(vals))
		for i := range vals {
			placeholders[i] = "?"
		}
		return col + " IN (" + strings.Join(placeholders, ", ") + ")", vals, nil

	case "not in":
		vals := toInterfaceSlice(value)
		if len(vals) == 0 {
			return "TRUE", nil, nil
		}
		placeholders := make([]string, len(vals))
		for i := range vals {
			placeholders[i] = "?"
		}
		return col + " NOT IN (" + strings.Join(placeholders, ", ") + ")", vals, nil

	case "ilike", "like":
		s := fmt.Sprintf("%v", value)
		s = strings.ReplaceAll(s, "%", "%%")
		return col + " ILIKE ?", []interface{}{"%" + s + "%"}, nil

	case "is":
		return col + " IS NULL", nil, nil

	case "is not":
		return col + " IS NOT NULL", nil, nil

	default:
		return "", nil, fmt.Errorf("unsupported operator: %s", op)
	}
}

// isCondition checks if the slice is a [field, operator, value] triple.
func isCondition(items []interface{}) bool {
	if len(items) != 3 {
		return false
	}
	_, fieldOk := items[0].(string)
	_, opOk := items[1].(string)
	return fieldOk && opOk
}

// QuoteIdent double-quotes a PostgreSQL identifier, handling dotted paths
// like "table.column" → `"table"."column"`.
func QuoteIdent(name string) string {
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		return fmt.Sprintf(`"%s"."%s"`, parts[0], parts[1])
	}
	return fmt.Sprintf(`"%s"`, name)
}

// toSlice attempts to convert an interface{} to []interface{}.
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

// toInterfaceSlice converts a value (expected to be a JSON array) to []interface{}.
func toInterfaceSlice(v interface{}) []interface{} {
	switch s := v.(type) {
	case []interface{}:
		return s
	case []string:
		r := make([]interface{}, len(s))
		for i, x := range s {
			r[i] = x
		}
		return r
	case []float64:
		r := make([]interface{}, len(s))
		for i, x := range s {
			r[i] = x
		}
		return r
	default:
		return nil
	}
}

func coerceToBool(v interface{}) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "t", "yes":
			return true, nil
		case "false", "0", "f", "no":
			return false, nil
		default:
			return false, fmt.Errorf("invalid boolean string %q", x)
		}
	case float64:
		if x == 0 {
			return false, nil
		}
		if x == 1 {
			return true, nil
		}
		return false, fmt.Errorf("invalid boolean number %v", x)
	default:
		return false, fmt.Errorf("cannot coerce %T to bool", v)
	}
}
