package filter

import (
	"fmt"
	"strings"

	"github.com/uptrace/bun"
)

// ApplyFuzzySearch adds a WHERE clause that concatenates the given fields with
// a separator, applies unaccent(), and matches with ILIKE.
//
// Matches Python's CommonController._fuzzy_search using the 🍔 separator
// and PostgreSQL's unaccent() extension.
func ApplyFuzzySearch(q *bun.SelectQuery, term string, fields []string) *bun.SelectQuery {
	if term == "" || len(fields) == 0 {
		return q
	}

	const sep = `'🍔'`

	parts := make([]string, 0, len(fields)*2+1)
	parts = append(parts, sep)
	for _, f := range fields {
		parts = append(parts, fmt.Sprintf(`COALESCE(CAST(%s AS TEXT), %s)`, QuoteIdent(f), sep))
		parts = append(parts, sep)
	}
	combined := strings.Join(parts, " || ")

	q = q.Where(
		fmt.Sprintf(`unaccent(%s) ILIKE unaccent(?)`, combined),
		"%"+term+"%",
	)
	return q
}
