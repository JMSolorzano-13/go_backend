package datetime

import (
	"testing"
	"time"
)

// fixed "now" in UTC; Mexico City is UTC-6 (no DST) → same calendar date as UTC for midday UTC.
func TestAdminEnqueueCalendarRangeClamped_FutureInclusiveEnd(t *testing.T) {
	now := time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
	start, endEx, eff, clamped, err := AdminEnqueueCalendarRangeClamped("2021-01-01", "2026-04-13", now)
	if err != nil {
		t.Fatal(err)
	}
	if !clamped {
		t.Fatal("expected end clamped")
	}
	if eff != "2026-04-12" {
		t.Fatalf("effective inclusive end: got %q want 2026-04-12 (Mexico calendar)", eff)
	}
	wantEndEx := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
	if !endEx.Equal(wantEndEx) {
		t.Fatalf("endExclusive: got %v want %v", endEx, wantEndEx)
	}
	if !start.Equal(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("start: %v", start)
	}
}

func TestAdminEnqueueCalendarRangeClamped_NoClampWhenEndOnToday(t *testing.T) {
	now := time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
	_, _, eff, clamped, err := AdminEnqueueCalendarRangeClamped("2021-01-02", "2026-04-12", now)
	if err != nil {
		t.Fatal(err)
	}
	if clamped {
		t.Fatal("expected no clamp")
	}
	if eff != "2026-04-12" {
		t.Fatalf("eff: %q", eff)
	}
}
