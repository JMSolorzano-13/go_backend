package filter

import "testing"

func BenchmarkBuildConditionSQL_efosAny(b *testing.B) {
	for b.Loop() {
		_, _, err := buildConditionSQL([]interface{}{"efos", "=", "any"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestBuildConditionSQL_efosAny(t *testing.T) {
	t.Parallel()
	sql, args, err := buildConditionSQL([]interface{}{"efos", "=", "any"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %v", args)
	}
	want := `"RfcEmisor" IN (SELECT "rfc" FROM "public"."efos")`
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
}

func TestBuildConditionSQL_efosNotAny(t *testing.T) {
	t.Parallel()
	sql, args, err := buildConditionSQL([]interface{}{"efos", "!=", "any"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %v", args)
	}
	want := `"RfcEmisor" NOT IN (SELECT "rfc" FROM "public"."efos")`
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
}
