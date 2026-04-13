package datetime

import (
	"testing"
	"time"
)

func TestMexicoCity_NotNil(t *testing.T) {
	if MexicoCity() == nil {
		t.Fatal("MexicoCity() is nil")
	}
}

func TestMXCalendarDate_NormalizesToUTCMidnight(t *testing.T) {
	in := time.Date(2026, 7, 4, 18, 30, 45, 0, MexicoCity())
	got := MXCalendarDate(in)
	want := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestLastXFiscalYearsStart_UsesMexicoYear(t *testing.T) {
	// Smoke: year component should be current MX year minus 5 (unless test runs exactly at year boundary edge).
	y := time.Now().In(MexicoCity()).Year()
	got := LastXFiscalYearsStart(5)
	if got.Year() != y-5 || got.Month() != time.January || got.Day() != 1 {
		t.Fatalf("unexpected LastXFiscalYearsStart(5): %v (mx year %d)", got, y)
	}
	if got.Location().String() != MexicoCity().String() {
		t.Fatalf("want America/Mexico_City location, got %v", got.Location())
	}
}

func TestADDDefaultSyncWindow_EndIsStartOfMonthOrAfter(t *testing.T) {
	start, end := ADDDefaultSyncWindow()
	if start.After(end) {
		t.Fatalf("start %v after end %v", start, end)
	}
}
