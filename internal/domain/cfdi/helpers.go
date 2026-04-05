package cfdi

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func fmtDate(t time.Time) string {
	return t.Format("2006-01-02")
}

func fmtTimestamp(t time.Time) string {
	return t.Format("2006-01-02T15:04:05")
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case nil:
		return 0
	default:
		return 0
	}
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case nil:
		return 0
	default:
		return 0
	}
}

func scanFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case []byte:
		var f float64
		fmt.Sscanf(string(val), "%f", &f)
		return f
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	case nil:
		return 0
	default:
		return 0
	}
}

func scanInt(v interface{}) int {
	switch val := v.(type) {
	case int64:
		return int(val)
	case int:
		return val
	case float64:
		return int(val)
	case nil:
		return 0
	default:
		return 0
	}
}

func endMonth(d time.Time) time.Time {
	y, m, _ := d.Date()
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC)
}

func joinOR(parts []string) string {
	if len(parts) == 0 {
		return "TRUE"
	}
	return strings.Join(parts, " OR ")
}

func strContains(s, substr string) bool {
	return strings.Contains(s, substr)
}
