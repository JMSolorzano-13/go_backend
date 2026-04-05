package crud

import (
	"encoding/json"
	"strings"
	"time"
)

// APITimestampFormat matches Python's datetime ISO serialization (no timezone).
// The frontend appends "Z" itself when parsing, so we must NOT include it.
const APITimestampFormat = "2006-01-02T15:04:05"

// Serialize converts a slice of model structs to []map[string]interface{},
// matching Python's CommonController.to_nested_dict.
//
// Uses json marshal/unmarshal for type conversion (time.Time → ISO8601, etc.).
func Serialize[T any](records []T) []map[string]interface{} {
	if len(records) == 0 {
		return []map[string]interface{}{}
	}
	result := make([]map[string]interface{}, 0, len(records))
	for _, r := range records {
		result = append(result, SerializeOne(r))
	}
	return result
}

// SerializeWithFields converts records and filters to only include the given fields.
func SerializeWithFields[T any](records []T, fields []string) []map[string]interface{} {
	if len(records) == 0 {
		return []map[string]interface{}{}
	}
	if len(fields) == 0 {
		return Serialize(records)
	}

	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}

	result := make([]map[string]interface{}, 0, len(records))
	for _, r := range records {
		full := SerializeOne(r)
		filtered := make(map[string]interface{}, len(fields))
		for k, v := range full {
			if fieldSet[k] {
				filtered[k] = v
			}
		}
		result = append(result, filtered)
	}
	return result
}

// SerializeOne converts a single struct to map[string]interface{}.
// Strips timezone indicators from date strings to match Python's output.
func SerializeOne(v interface{}) map[string]interface{} {
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]interface{}{}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]interface{}{}
	}
	stripDateTimezones(m)
	return m
}

// NestDottedKeys converts flat dotted keys to nested maps:
// {"a.b": "v"} → {"a": {"b": "v"}}
func NestDottedKeys(flat map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range flat {
		parts := strings.Split(key, ".")
		if len(parts) == 1 {
			result[key] = value
			continue
		}
		current := result
		for i, part := range parts[:len(parts)-1] {
			if _, exists := current[part]; !exists {
				current[part] = make(map[string]interface{})
			}
			if next, ok := current[part].(map[string]interface{}); ok {
				current = next
			} else {
				nested := make(map[string]interface{})
				current[part] = nested
				current = nested
				_ = i
			}
		}
		current[parts[len(parts)-1]] = value
	}
	return result
}

// normalizeValue converts database driver types to JSON-friendly Go types.
func normalizeValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case time.Time:
		if val.IsZero() {
			return nil
		}
		return val.Format(APITimestampFormat)
	case *time.Time:
		if val == nil || val.IsZero() {
			return nil
		}
		return val.Format(APITimestampFormat)
	case []byte:
		var j interface{}
		if err := json.Unmarshal(val, &j); err == nil {
			return j
		}
		return string(val)
	default:
		return val
	}
}

// stripDateTimezones recursively walks a map and strips timezone suffixes
// from ISO 8601 date strings (Go's json.Marshal uses RFC3339 for time.Time).
func stripDateTimezones(m map[string]interface{}) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if stripped := stripTZ(val); stripped != val {
				m[k] = stripped
			}
		case map[string]interface{}:
			stripDateTimezones(val)
		case []interface{}:
			for i, item := range val {
				switch inner := item.(type) {
				case map[string]interface{}:
					stripDateTimezones(inner)
				case string:
					if stripped := stripTZ(inner); stripped != inner {
						val[i] = stripped
					}
				}
			}
		}
	}
}

// stripTZ removes timezone indicators (Z, +HH:MM, -HH:MM) from ISO 8601 timestamps,
// preserving fractional seconds. E.g. "2026-03-24T03:21:58.123Z" → "2026-03-24T03:21:58.123"
func stripTZ(s string) string {
	n := len(s)
	if n < 20 || s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':' {
		return s
	}
	for i := 19; i < n; i++ {
		c := s[i]
		if c == 'Z' || c == '+' || c == '-' {
			return s[:i]
		}
		if c != '.' && (c < '0' || c > '9') {
			return s
		}
	}
	return s
}
