package datetime

import (
	"testing"
	"time"
)

func TestChunkRangeByDays(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 10)
	chunks := ChunkRangeByDays(start, end, 3)
	if len(chunks) != 4 {
		t.Fatalf("want 4 chunks, got %d %+v", len(chunks), chunks)
	}
	want0End := start.AddDate(0, 0, 3)
	if !chunks[0].Start.Equal(start) || !chunks[0].End.Equal(want0End) {
		t.Fatalf("chunk0: %+v", chunks[0])
	}
	if !chunks[3].End.Equal(end) {
		t.Fatalf("last chunk end: %+v", chunks[3])
	}
}

func TestChunkRangeByDays_Invalid(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if ChunkRangeByDays(start, start, 30) != nil {
		t.Fatal("expected nil")
	}
	if ChunkRangeByDays(start, start.AddDate(0, 0, 1), 0) != nil {
		t.Fatal("expected nil for chunkDays<=0")
	}
}
