package cfdi

import (
	"database/sql"
	"testing"
	"time"
)

func TestDomainCopyForExerciseUsesEarliestLowerBoundYear(t *testing.T) {
	domain := []interface{}{
		[]interface{}{"company_identifier", "=", "cid"},
		[]interface{}{"FechaFiltro", ">=", "2024-06-01T00:00:00.000"},
		[]interface{}{"FechaFiltro", "<", "2025-01-01T00:00:00.000"},
	}
	max := sql.NullTime{Time: time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC), Valid: true}
	out := domainCopyForExercise(domain, max)
	if len(out) != 3 {
		t.Fatalf("expected 3 triples, got %d", len(out))
	}
	last := out[len(out)-1].([]interface{})
	if last[0] != "FechaFiltro" || last[1] != ">=" {
		t.Fatalf("expected FechaFiltro lower bound appended, got %#v", last)
	}
	want := "2024-01-01T00:00:00"
	if last[2] != want {
		t.Fatalf("expected %q, got %q", want, last[2])
	}
}

func TestDomainCopyForExerciseFallsBackToMaxWhenNoLowerBound(t *testing.T) {
	domain := []interface{}{
		[]interface{}{"FechaFiltro", "<", "2025-01-01T00:00:00.000"},
	}
	max := sql.NullTime{Time: time.Date(2024, 8, 1, 12, 0, 0, 0, time.UTC), Valid: true}
	out := domainCopyForExercise(domain, max)
	last := out[len(out)-1].([]interface{})
	if last[2] != "2024-01-01T00:00:00" {
		t.Fatalf("expected Jan 1 2024 from max date year, got %q", last[2])
	}
}
