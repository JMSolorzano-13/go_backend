package datetime

import "time"

// SATDateChunk is a half-open UTC window [Start, End) used for SAT SolicitaDescarga ranges.
type SATDateChunk struct {
	Start time.Time
	End   time.Time
}

// ChunkRangeByDays splits [start, endExclusive) into consecutive windows of at most chunkDays
// calendar days each (AddDate), avoiding 24h steps that drift across historical Mexico DST transitions.
func ChunkRangeByDays(start, endExclusive time.Time, chunkDays int) []SATDateChunk {
	if chunkDays <= 0 || !start.Before(endExclusive) {
		return nil
	}
	var out []SATDateChunk
	cursor := start
	for cursor.Before(endExclusive) {
		ce := cursor.AddDate(0, 0, chunkDays)
		if ce.After(endExclusive) {
			ce = endExclusive
		}
		out = append(out, SATDateChunk{Start: cursor, End: ce})
		cursor = ce
	}
	return out
}
