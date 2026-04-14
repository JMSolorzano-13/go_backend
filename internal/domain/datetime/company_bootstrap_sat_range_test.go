package datetime

import (
	"testing"
	"time"
)

func TestCompanyBootstrapSATRangeUTC_MatchesAdminStyleWindow(t *testing.T) {
	// 2026-06-15 12:00 UTC → Mexico still 2026-06-15; yesterday inclusive 2026-06-14;
	// start (5y) = 2021-01-02 — same shape as working /api/admin/sat-enqueue payloads.
	fixed := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	start, endEx, eff, clamped, err := CompanyBootstrapSATRangeUTC(fixed, 5)
	if err != nil {
		t.Fatal(err)
	}
	if clamped {
		t.Fatalf("did not expect clamp, got endWasClamped")
	}
	if eff != "2026-06-14" {
		t.Fatalf("endInclusiveEffective: got %q", eff)
	}
	wantStart := time.Date(2021, 1, 2, 0, 0, 0, 0, time.UTC)
	wantEndEx := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !endEx.Equal(wantEndEx) {
		t.Fatalf("start=%v endEx=%v want start=%v endEx=%v", start, endEx, wantStart, wantEndEx)
	}
}
