package sat

import (
	"testing"
	"time"
)

func TestSoapFechaCentro_UTCMidnightLabelMatchesMexicoMidnightString(t *testing.T) {
	// Admin/bootstrap store 2021-01-01 as UTC midnight; SAT expects same calendar at 00:00 Centro.
	utcLabel := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	got := soapFechaCentro(utcLabel)
	if got != "2021-01-01T00:00:00" {
		t.Fatalf("got %q", got)
	}
}

func TestSoapFechaCentro_MexicoMidnightJan1(t *testing.T) {
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		t.Skip(err)
	}
	mxMid := time.Date(2021, 1, 1, 0, 0, 0, 0, loc)
	got := soapFechaCentro(mxMid)
	if got != "2021-01-01T00:00:00" {
		t.Fatalf("got %q", got)
	}
}
