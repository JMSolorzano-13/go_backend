package datetime

import (
	"fmt"
	"time"
)

// CompanyBootstrapSATRangeUTC returns the half-open UTC window [start, endExclusive) for
// first-company SAT bootstrap and SendQueryMetadata. It mirrors successful
// POST /api/admin/sat-enqueue usage in the field:
//   - inclusive start calendar day: January 2 of (Mexico calendar year − years)
//   - inclusive end calendar day: Mexico "yesterday" (America/Mexico_City), then parsed
//     with AdminEnqueueCalendarRangeClamped so the exclusive upper bound matches admin.
//
// years is typically 5 (same fiscal lookback as LastXFiscalYearsStart).
func CompanyBootstrapSATRangeUTC(now time.Time, years int) (start, endExclusive time.Time, endInclusiveEffective string, endWasClamped bool, err error) {
	if years < 1 {
		return time.Time{}, time.Time{}, "", false, fmt.Errorf("years must be >= 1")
	}
	mx := now.In(MexicoCity())
	startStr := fmt.Sprintf("%04d-01-02", mx.Year()-years)
	todayMX := MXCalendarDate(mx)
	endInclusive := todayMX.AddDate(0, 0, -1)
	endStr := endInclusive.Format("2006-01-02")
	return AdminEnqueueCalendarRangeClamped(startStr, endStr, now)
}
