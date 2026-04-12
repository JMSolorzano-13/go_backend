package datetime

import "time"

// SATDateChunk is a half-open UTC window [Start, End) used for SAT SolicitaDescarga ranges.
type SATDateChunk struct {
	Start time.Time
	End   time.Time
}

// ChunkRangeByDays splits [start, endExclusive) into consecutive windows of at most chunkDays
// each using a fixed 24h step (matches admin SAT enqueue and legacy Python tooling).
func ChunkRangeByDays(start, endExclusive time.Time, chunkDays int) []SATDateChunk {
	if chunkDays <= 0 || !start.Before(endExclusive) {
		return nil
	}
	delta := time.Duration(chunkDays) * 24 * time.Hour
	var out []SATDateChunk
	cursor := start
	for cursor.Before(endExclusive) {
		ce := cursor.Add(delta)
		if ce.After(endExclusive) {
			ce = endExclusive
		}
		out = append(out, SATDateChunk{Start: cursor, End: ce})
		cursor = ce
	}
	return out
}
