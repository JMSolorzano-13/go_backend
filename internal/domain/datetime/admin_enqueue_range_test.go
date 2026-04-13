package datetime

import (
	"testing"
	"time"
)

func TestAdminEnqueueCalendarRange_MetadataYearBounds(t *testing.T) {
	start, endEx, err := AdminEnqueueCalendarRange("2025-01-01", "2025-12-31")
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	wantEndEx := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !endEx.Equal(wantEndEx) {
		t.Fatalf("got start=%v endEx=%v want start=%v endEx=%v", start, endEx, wantStart, wantEndEx)
	}
	chunks := ChunkRangeByDays(start, endEx, 20)
	if len(chunks) != 19 {
		t.Fatalf("chunk count: got %d want 19 (same as admin sat-enqueue for 2025 full year, 20-day chunks)", len(chunks))
	}
	first := chunks[0]
	if !first.Start.Equal(wantStart) {
		t.Fatalf("first chunk start: %v", first.Start)
	}
	last := chunks[len(chunks)-1]
	if !last.End.Equal(wantEndEx) {
		t.Fatalf("last chunk end: got %v want %v", last.End, wantEndEx)
	}
}

func TestAdminEnqueueCalendarRange_SingleDay(t *testing.T) {
	start, endEx, err := AdminEnqueueCalendarRange("2025-06-15", "2025-06-15")
	if err != nil {
		t.Fatal(err)
	}
	chunks := ChunkRangeByDays(start, endEx, 30)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d %+v", len(chunks), chunks)
	}
	if !chunks[0].Start.Equal(start) || !chunks[0].End.Equal(endEx) {
		t.Fatalf("chunk: %+v", chunks[0])
	}
}

func TestAdminEnqueueCalendarRange_LeapDay(t *testing.T) {
	start, endEx, err := AdminEnqueueCalendarRange("2024-02-28", "2024-03-01")
	if err != nil {
		t.Fatal(err)
	}
	// Inclusive end 2024-03-01 → exclusive 2024-03-02
	wantEndEx := time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)
	if !endEx.Equal(wantEndEx) {
		t.Fatalf("endExclusive: %v", endEx)
	}
	chunks := ChunkRangeByDays(start, endEx, 1)
	if len(chunks) != 3 {
		t.Fatalf("1-day chunks from Feb28–Mar1 inclusive: want 3 days, got %d", len(chunks))
	}
}

func TestAdminEnqueueCalendarRange_InvalidOrder(t *testing.T) {
	_, _, err := AdminEnqueueCalendarRange("2025-12-31", "2025-01-01")
	if err == nil {
		t.Fatal("expected error")
	}
}
