package event

import (
	"testing"

	"github.com/siigofiscal/go_backend/internal/config"
)

type seqRecorder struct {
	sequence []EventType
	payloads []DomainEvent
}

func (r *seqRecorder) subscribe(bus *Bus, et EventType) {
	bus.Subscribe(et, &typedRecorder{et: et, out: r})
}

type typedRecorder struct {
	et  EventType
	out *seqRecorder
}

func (tr *typedRecorder) Handle(ev DomainEvent) error {
	tr.out.sequence = append(tr.out.sequence, tr.et)
	tr.out.payloads = append(tr.out.payloads, ev)
	return nil
}

func TestOnCompanyCreateAutoSync_LocalInfra_PublishesMetadataAndCompleteCFDIs(t *testing.T) {
	bus := NewBus(false)
	var rec seqRecorder
	rec.subscribe(bus, EventTypeSATMetadataRequested)
	rec.subscribe(bus, EventTypeSATCompleteCFDIsNeeded)
	rec.subscribe(bus, EventTypeRequestScrap)

	h := &OnCompanyCreateAutoSync{Bus: bus, Cfg: &config.Config{LocalInfra: true}}
	err := h.Handle(CompanyCreatedEvent{
		CompanyIdentifier: "00000000-0000-0000-0000-000000000001",
		CompanyRFC:        "XAXX010101000",
		WorkspaceID:       42,
		CompanyID:         7,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	want := []EventType{
		EventTypeSATMetadataRequested,
		EventTypeSATCompleteCFDIsNeeded,
		EventTypeSATCompleteCFDIsNeeded,
	}
	if len(rec.sequence) != len(want) {
		t.Fatalf("got %d publishes, want %d: %v", len(rec.sequence), len(want), rec.sequence)
	}
	for i, et := range want {
		if rec.sequence[i] != et {
			t.Errorf("publish[%d]: got %s, want %s", i, rec.sequence[i], et)
		}
	}

	meta, ok := rec.payloads[0].(SQSCompanySendMetadata)
	if !ok || meta.CompanyIdentifier != "00000000-0000-0000-0000-000000000001" || meta.CompanyRFC != "XAXX010101000" || meta.WID != 42 || meta.CID != 7 {
		t.Fatalf("metadata payload: %+v", rec.payloads[0])
	}

	for i := 1; i <= 2; i++ {
		nc, ok := rec.payloads[i].(NeedToCompleteCFDIsEvent)
		if !ok || nc.IsManual {
			t.Fatalf("complete CFDIs payload[%d]: %+v", i, rec.payloads[i])
		}
	}
	issued := rec.payloads[1].(NeedToCompleteCFDIsEvent)
	recv := rec.payloads[2].(NeedToCompleteCFDIsEvent)
	if issued.DownloadType != "ISSUED" || recv.DownloadType != "RECEIVED" {
		t.Fatalf("download types: issued=%q received=%q", issued.DownloadType, recv.DownloadType)
	}
	if issued.Start == nil || recv.Start == nil {
		t.Fatal("expected Start on complete-CFDI events")
	}
}

func TestOnCompanyCreateAutoSync_NonLocal_PublishesScrap(t *testing.T) {
	bus := NewBus(false)
	var rec seqRecorder
	rec.subscribe(bus, EventTypeSATMetadataRequested)
	rec.subscribe(bus, EventTypeSATCompleteCFDIsNeeded)
	rec.subscribe(bus, EventTypeRequestScrap)

	h := &OnCompanyCreateAutoSync{Bus: bus, Cfg: &config.Config{LocalInfra: false}}
	if err := h.Handle(CompanyCreatedEvent{CompanyIdentifier: "cid", CompanyRFC: "RFC", WorkspaceID: 1, CompanyID: 2}); err != nil {
		t.Fatal(err)
	}

	if len(rec.sequence) != 4 {
		t.Fatalf("sequence: %v", rec.sequence)
	}
	if rec.sequence[3] != EventTypeRequestScrap {
		t.Fatalf("last event: %s", rec.sequence[3])
	}
	if _, ok := rec.payloads[3].(scrapRequestStub); !ok {
		t.Fatalf("scrap payload type: %T", rec.payloads[3])
	}
}
