package handler

import "testing"

func TestPastoCompanyRowsFromBody(t *testing.T) {
	t.Parallel()
	if got := pastoCompanyRowsFromBody([]interface{}{map[string]interface{}{"x": 1}}); len(got) != 1 {
		t.Fatalf("array: %v", got)
	}
	if got := pastoCompanyRowsFromBody(map[string]interface{}{"companies": []interface{}{}}); len(got) != 0 {
		t.Fatalf("empty companies: %v", got)
	}
	if got := pastoCompanyRowsFromBody(map[string]interface{}{"data": []interface{}{map[string]interface{}{}}}); len(got) != 1 {
		t.Fatalf("data key: %v", got)
	}
	if got := pastoCompanyRowsFromBody(map[string]interface{}{}); got != nil {
		t.Fatalf("bare object: %v", got)
	}
}
