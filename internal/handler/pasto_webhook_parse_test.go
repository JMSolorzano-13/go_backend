package handler

import (
	"net/http"
	"testing"
)

func TestParsePastoWebhookRaw_Success(t *testing.T) {
	body := map[string]interface{}{
		"Status": float64(0),
		"Body":   `{"DbServerName":"srv","DbUsername":"u","DbPassword":"p"}`,
	}
	hasErr, raw, _ := parsePastoWebhookRaw(body, map[string]interface{}{}, "test")
	if hasErr {
		t.Fatal("expected success")
	}
	m, ok := raw.(map[string]interface{})
	if !ok || m["DbServerName"] != "srv" {
		t.Fatalf("parsed body: %#v", raw)
	}
}

func TestParsePastoWebhookRaw_NonZeroStatus(t *testing.T) {
	body := map[string]interface{}{"Status": float64(1), "Body": `{}`}
	hasErr, raw, _ := parsePastoWebhookRaw(body, map[string]interface{}{}, "test")
	if !hasErr || raw != nil {
		t.Fatalf("hasErr=%v raw=%v", hasErr, raw)
	}
}

func TestParsePastoWebhookRaw_InvalidInnerJSON(t *testing.T) {
	body := map[string]interface{}{"Status": float64(0), "Body": `{`}
	hasErr, raw, _ := parsePastoWebhookRaw(body, map[string]interface{}{}, "test")
	if !hasErr || raw != nil {
		t.Fatalf("hasErr=%v raw=%v", hasErr, raw)
	}
}

func TestParseADDSyncDate(t *testing.T) {
	d, ok := parseADDSyncDate("2024-06-15")
	if !ok || d.Year() != 2024 || d.Month() != 6 || d.Day() != 15 {
		t.Fatalf("date only: %v %v", d, ok)
	}
	d, ok = parseADDSyncDate("2024-06-15T10:30:00Z")
	if !ok || d.Year() != 2024 || d.Month() != 6 || d.Day() != 15 {
		t.Fatalf("rfc3339: %v %v", d, ok)
	}
}

func TestHeaderStr_FromHTTPHeader(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("workspace_identifier", "ws-1")
	r.Header.Set("Worker_id", "w99")
	m := extractRequestHeaders(r)
	if headerStr(m, "workspace_identifier") != "ws-1" {
		t.Fatal("workspace_identifier")
	}
	if headerStr(m, "worker_id") != "w99" {
		t.Fatal("worker_id alias")
	}
}
