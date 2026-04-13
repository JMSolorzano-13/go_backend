package event

import (
	"testing"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/domain/datetime"
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
	rec.subscribe(bus, EventTypeSATWSRequestCreateQuery)
	rec.subscribe(bus, EventTypeSATCompleteCFDIsNeeded)
	rec.subscribe(bus, EventTypeRequestScrap)

	fixedNow := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	start := datetime.LastXFiscalYearsStart(5)
	endEx := datetime.MXCalendarDate(fixedNow.In(datetime.MexicoCity())).AddDate(0, 0, 1)
	nWin := len(datetime.ChunkRangeByDays(start, endEx, initialCompanyCFDIChunkDays))
	nCreate := nWin * 2

	h := &OnCompanyCreateAutoSync{
		Bus: bus, Cfg: &config.Config{LocalInfra: true},
		TimeNow: func() time.Time { return fixedNow },
	}
	err := h.Handle(CompanyCreatedEvent{
		CompanyIdentifier: "00000000-0000-0000-0000-000000000001",
		CompanyRFC:        "XAXX010101000",
		WorkspaceID:       42,
		CompanyID:         7,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	wantLen := 1 + nCreate + 2
	if len(rec.sequence) != wantLen {
		t.Fatalf("got %d publishes, want %d: %v", len(rec.sequence), wantLen, rec.sequence)
	}
	if rec.sequence[0] != EventTypeSATMetadataRequested {
		t.Fatalf("first event: %s", rec.sequence[0])
	}
	for i := 1; i <= nCreate; i++ {
		if rec.sequence[i] != EventTypeSATWSRequestCreateQuery {
			t.Fatalf("publish[%d]: want create-query, got %s", i, rec.sequence[i])
		}
	}
	if rec.sequence[1+nCreate] != EventTypeSATCompleteCFDIsNeeded ||
		rec.sequence[2+nCreate] != EventTypeSATCompleteCFDIsNeeded {
		t.Fatalf("complete events: %v", rec.sequence[1+nCreate:])
	}

	meta, ok := rec.payloads[0].(SQSCompanySendMetadata)
	if !ok || meta.CompanyIdentifier != "00000000-0000-0000-0000-000000000001" || meta.CompanyRFC != "XAXX010101000" || meta.WID != 42 || meta.CID != 7 {
		t.Fatalf("metadata payload: %+v", rec.payloads[0])
	}
	if meta.ExecuteAt == nil || !meta.ExecuteAt.Equal(fixedNow) {
		t.Fatalf("metadata ExecuteAt: got %v want %v", meta.ExecuteAt, fixedNow)
	}

	for i := 1; i <= nCreate; i++ {
		q := rec.payloads[i].(QueryCreateEvent)
		if q.CompanyIdentifier != "00000000-0000-0000-0000-000000000001" || q.WID != 42 || q.CID != 7 {
			t.Fatalf("create-query[%d]: %+v", i, q)
		}
		want := fixedNow.Add(time.Duration(i) * SatSolicitudEnqueueSpacing)
		if q.ExecuteAt == nil || !q.ExecuteAt.Equal(want) {
			t.Fatalf("create-query[%d] ExecuteAt: got %v want %v", i, q.ExecuteAt, want)
		}
	}

	completeIssued := rec.payloads[1+nCreate].(NeedToCompleteCFDIsEvent)
	completeRecv := rec.payloads[2+nCreate].(NeedToCompleteCFDIsEvent)
	if completeIssued.DownloadType != "ISSUED" || completeRecv.DownloadType != "RECEIVED" {
		t.Fatalf("download types: issued=%q received=%q", completeIssued.DownloadType, completeRecv.DownloadType)
	}
	if completeIssued.Start == nil || completeRecv.Start == nil {
		t.Fatal("expected Start on complete-CFDI events")
	}
}

func TestOnCompanyCreateAutoSync_NonLocal_PublishesScrap(t *testing.T) {
	bus := NewBus(false)
	var rec seqRecorder
	rec.subscribe(bus, EventTypeSATMetadataRequested)
	rec.subscribe(bus, EventTypeSATWSRequestCreateQuery)
	rec.subscribe(bus, EventTypeSATCompleteCFDIsNeeded)
	rec.subscribe(bus, EventTypeRequestScrap)

	fixedNow := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	start := datetime.LastXFiscalYearsStart(5)
	endEx := datetime.MXCalendarDate(fixedNow.In(datetime.MexicoCity())).AddDate(0, 0, 1)
	nCreate := len(datetime.ChunkRangeByDays(start, endEx, initialCompanyCFDIChunkDays)) * 2

	h := &OnCompanyCreateAutoSync{
		Bus: bus, Cfg: &config.Config{LocalInfra: false},
		TimeNow: func() time.Time { return fixedNow },
	}
	if err := h.Handle(CompanyCreatedEvent{CompanyIdentifier: "cid", CompanyRFC: "RFC", WorkspaceID: 1, CompanyID: 2}); err != nil {
		t.Fatal(err)
	}

	wantLen := 1 + nCreate + 2 + 1
	if len(rec.sequence) != wantLen {
		t.Fatalf("sequence len %d want %d: %v", len(rec.sequence), wantLen, rec.sequence)
	}
	if rec.sequence[wantLen-1] != EventTypeRequestScrap {
		t.Fatalf("last event: %s", rec.sequence[wantLen-1])
	}
	if _, ok := rec.payloads[wantLen-1].(scrapRequestStub); !ok {
		t.Fatalf("scrap payload type: %T", rec.payloads[wantLen-1])
	}
}
