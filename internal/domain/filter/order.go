package filter

import (
	"fmt"
	"strings"

	"github.com/uptrace/bun"
)

// ApplyOrderBy parses an ORDER BY specification (comma-separated, each part
// being "column [ASC|DESC]") and applies it to the query with NULLS FIRST
// for ASC and NULLS LAST for DESC, matching Python's CommonController._apply_order_by.
func ApplyOrderBy(q *bun.SelectQuery, orderBy string) *bun.SelectQuery {
	if orderBy == "" {
		return q
	}

	for _, part := range strings.Split(orderBy, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		components := strings.Fields(part)
		column := components[0]
		direction := "ASC"
		if len(components) > 1 {
			direction = strings.ToUpper(components[1])
		}
		if direction != "ASC" && direction != "DESC" {
			direction = "ASC"
		}

		nullOrder := "NULLS FIRST"
		if direction == "DESC" {
			nullOrder = "NULLS LAST"
		}

		column = strings.ReplaceAll(column, `"`, "")
		q = q.OrderExpr(fmt.Sprintf(`%s %s %s`, QuoteIdent(column), direction, nullOrder))
	}
	return q
}
