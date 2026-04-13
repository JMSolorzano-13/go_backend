package datetime

import (
	"fmt"
	"time"
)

// AdminEnqueueCalendarRange parses inclusive YYYY-MM-DD calendar bounds into the
// half-open UTC window [start, endExclusive) used by ChunkRangeByDays and SAT enqueue.
//
// Each date is interpreted as the calendar day whose components match the string,
// at 00:00:00 UTC — the same convention as MXCalendarDate for stored DATE-like values
// (numeric Y-M-D, not "midnight in Mexico City" converted to UTC).
func AdminEnqueueCalendarRange(startStr, endStr string) (start time.Time, endExclusive time.Time, err error) {
	start, err = time.Parse("2006-01-02", startStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start: %w", err)
	}
	endDay, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("end: %w", err)
	}
	endExclusive = endDay.AddDate(0, 0, 1)
	start = start.UTC()
	endExclusive = endExclusive.UTC()
	if !start.Before(endExclusive) {
		return time.Time{}, time.Time{}, fmt.Errorf("start must be before or equal to end")
	}
	return start, endExclusive, nil
}

// AdminEnqueueCalendarRangeClamped is like AdminEnqueueCalendarRange but caps the inclusive
// end date to the current calendar day in America/Mexico_City (same as MXCalendarDate(now)).
// SAT rejects download windows that extend into future calendar days; clients may see HTTP 301.
func AdminEnqueueCalendarRangeClamped(startStr, endStr string, now time.Time) (
	start time.Time,
	endExclusive time.Time,
	endInclusiveEffective string,
	endWasClamped bool,
	err error,
) {
	start, err = time.Parse("2006-01-02", startStr)
	if err != nil {
		return time.Time{}, time.Time{}, "", false, fmt.Errorf("start: %w", err)
	}
	endDay, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return time.Time{}, time.Time{}, "", false, fmt.Errorf("end: %w", err)
	}
	start = start.UTC()
	endDay = endDay.UTC()

	todayMX := MXCalendarDate(now.In(MexicoCity()))
	if endDay.After(todayMX) {
		endDay = todayMX
		endWasClamped = true
	}
	endInclusiveEffective = endDay.Format("2006-01-02")
	endExclusive = endDay.AddDate(0, 0, 1)
	if !start.Before(endExclusive) {
		return time.Time{}, time.Time{}, "", endWasClamped, fmt.Errorf("start must be before or equal to effective end (after capping end to Mexico today)")
	}
	return start, endExclusive, endInclusiveEffective, endWasClamped, nil
}
