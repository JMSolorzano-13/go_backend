package handler

import (
	"testing"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/model/control"
)

type evtRecorder struct {
	types []event.EventType
}

func (e *evtRecorder) makeHandler(et event.EventType) event.EventHandler {
	return handlerFunc(func(ev event.DomainEvent) error {
		e.types = append(e.types, et)
		return nil
	})
}

type handlerFunc func(event.DomainEvent) error

func (f handlerFunc) Handle(ev event.DomainEvent) error { return f(ev) }

func TestSATQuery_publishManualSATEvents_CFIDIssuedReceivedMetadataScrap(t *testing.T) {
	bus := event.NewBus(false)
	var rec evtRecorder
	bus.Subscribe(event.EventTypeSATWSRequestCreateQuery, rec.makeHandler(event.EventTypeSATWSRequestCreateQuery))
	bus.Subscribe(event.EventTypeSATMetadataRequested, rec.makeHandler(event.EventTypeSATMetadataRequested))
	bus.Subscribe(event.EventTypeRequestScrap, rec.makeHandler(event.EventTypeRequestScrap))

	wid := int64(99)
	rfc := "AAA010101AAA"
	company := &control.Company{
		ID:          5,
		Identifier:  "11111111-1111-1111-1111-111111111111",
		WorkspaceID: &wid,
		RFC:         &rfc,
	}
	h := &SATQuery{cfg: &config.Config{}, database: nil, bus: bus}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mxNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h.publishManualSATEvents(company, start, mxNow)

	want := []event.EventType{
		event.EventTypeSATWSRequestCreateQuery,
		event.EventTypeSATWSRequestCreateQuery,
		event.EventTypeSATMetadataRequested,
		event.EventTypeRequestScrap,
	}
	if len(rec.types) != len(want) {
		t.Fatalf("got %v", rec.types)
	}
	for i := range want {
		if rec.types[i] != want[i] {
			t.Errorf("[%d] got %s want %s", i, rec.types[i], want[i])
		}
	}
}

func TestSATQuery_publishManualSATEvents_QueryCreatePayloads(t *testing.T) {
	bus := event.NewBus(false)
	var creates []event.QueryCreateEvent
	bus.Subscribe(event.EventTypeSATWSRequestCreateQuery, handlerFunc(func(ev event.DomainEvent) error {
		creates = append(creates, ev.(event.QueryCreateEvent))
		return nil
	}))
	bus.Subscribe(event.EventTypeSATMetadataRequested, handlerFunc(func(event.DomainEvent) error { return nil }))
	bus.Subscribe(event.EventTypeRequestScrap, handlerFunc(func(event.DomainEvent) error { return nil }))

	wid := int64(1)
	company := &control.Company{ID: 2, Identifier: "u", WorkspaceID: &wid}
	h := &SATQuery{bus: bus}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	h.publishManualSATEvents(company, start, end)

	if len(creates) != 2 {
		t.Fatalf("creates: %d", len(creates))
	}
	pairs := [][2]string{{creates[0].RequestType, creates[0].DownloadType}, {creates[1].RequestType, creates[1].DownloadType}}
	if pairs[0][0] != "CFDI" || pairs[1][0] != "CFDI" {
		t.Fatalf("request types: %+v", pairs)
	}
	got := map[string]bool{pairs[0][1]: true, pairs[1][1]: true}
	if !got["ISSUED"] || !got["RECEIVED"] {
		t.Fatalf("download types: %+v", pairs)
	}
	for _, q := range creates {
		if !q.IsManual || q.WID != wid || q.CID != company.ID || q.CompanyIdentifier != "u" {
			t.Fatalf("unexpected create payload: %+v", q)
		}
	}
}

func TestSATQuery_publishManualSATEvents_MetadataPayload(t *testing.T) {
	bus := event.NewBus(false)
	bus.Subscribe(event.EventTypeSATWSRequestCreateQuery, handlerFunc(func(event.DomainEvent) error { return nil }))
	var meta event.SQSCompanySendMetadata
	bus.Subscribe(event.EventTypeSATMetadataRequested, handlerFunc(func(ev event.DomainEvent) error {
		meta = ev.(event.SQSCompanySendMetadata)
		return nil
	}))
	bus.Subscribe(event.EventTypeRequestScrap, handlerFunc(func(event.DomainEvent) error { return nil }))

	wid := int64(3)
	rfc := "BBB020202BBB"
	company := &control.Company{ID: 9, Identifier: "c-id", WorkspaceID: &wid, RFC: &rfc}
	h := &SATQuery{bus: bus}
	t0 := time.Now().UTC().Truncate(time.Minute)
	h.publishManualSATEvents(company, t0, t0)

	if !meta.ManuallyTriggered || meta.CompanyIdentifier != "c-id" || meta.CompanyRFC != rfc || meta.WID != wid || meta.CID != 9 {
		t.Fatalf("metadata: %+v", meta)
	}
}
