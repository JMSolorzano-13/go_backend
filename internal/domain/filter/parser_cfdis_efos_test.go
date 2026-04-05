package filter

import "testing"

func TestBuildConditionSQL_cfdisIsIssuedEqFalseString(t *testing.T) {
	t.Parallel()
	sql, args, err := buildConditionSQL([]interface{}{"cfdis.is_issued", "=", "false"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || args[0] != false {
		t.Fatalf("args: want [false], got %#v", args)
	}
	const want = `EXISTS (SELECT 1 FROM "cfdi" AS _cfdi_ic WHERE _cfdi_ic."RfcEmisor" = "e"."rfc" AND _cfdi_ic."is_issued" = ?)`
	if sql != want {
		t.Fatalf("got %q\nwant %q", sql, want)
	}
}

func TestBuildConditionSQL_cfdisIsIssuedNeFalse(t *testing.T) {
	t.Parallel()
	sql, args, err := buildConditionSQL([]interface{}{"cfdis.is_issued", "!=", "false"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || args[0] != true {
		t.Fatalf("args: want [true], got %#v", args)
	}
	const want = `EXISTS (SELECT 1 FROM "cfdi" AS _cfdi_ic WHERE _cfdi_ic."RfcEmisor" = "e"."rfc" AND _cfdi_ic."is_issued" = ?)`
	if sql != want {
		t.Fatalf("got %q\nwant %q", sql, want)
	}
}
