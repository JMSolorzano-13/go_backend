package datetime

import "time"

// MexicoCity returns America/Mexico_City, or UTC if the tz database is unavailable.
func MexicoCity() *time.Location {
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		return time.UTC
	}
	return loc
}

// MXCalendarDate returns year/month/day from t interpreted in the Mexico City zone,
// stored as UTC midnight (matches DATE columns fed from Python mx_now().date()).
func MXCalendarDate(t time.Time) time.Time {
	t = t.In(MexicoCity())
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// LastXFiscalYearsStart matches Python last_X_fiscal_years: Jan 1 of (current MX calendar year − years).
func LastXFiscalYearsStart(years int) time.Time {
	n := time.Now().In(MexicoCity())
	return time.Date(n.Year()-years, time.January, 1, 0, 0, 0, 0, time.UTC)
}

// ADDDefaultSyncWindow matches ADDSyncRequester.add_time_window_to_sync (Mexico “today”),
// including previous month when day ≤ 3 (Python’s January case is handled via AddDate).
func ADDDefaultSyncWindow() (start, end time.Time) {
	n := time.Now().In(MexicoCity())
	end = MXCalendarDate(n)
	start = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC)
	if n.Day() <= 3 {
		start = start.AddDate(0, -1, 0)
	}
	return start, end
}
