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
