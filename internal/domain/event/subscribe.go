package event

import (
	"log/slog"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/domain/datetime"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// LogRegistrations logs a summary of registered handlers at startup (WARN level
// so it is visible even when LOG_LEVEL=WARNING in .env).
func LogRegistrations(bus *Bus) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	total := 0
	for et, hs := range bus.handlers {
		slog.Warn("event_bus: registered", "event_type", string(et), "handlers", len(hs))
		total += len(hs)
	}
	slog.Warn("event_bus: registration complete", "event_types", len(bus.handlers), "total_handlers", total)
}

// -----------------------------------------------------------------------
// CompanyCreatedEvent — passed by Phase 7 company creation handler
// -----------------------------------------------------------------------

// CompanyCreatedEvent carries all data needed by onCompanyCreateAutoSync.
type CompanyCreatedEvent struct {
	CompanyIdentifier string
	CompanyRFC        string
	WorkspaceID       int64
	CompanyID         int64
}

// -----------------------------------------------------------------------
// onCompanyCreateAutoSync
// Mirrors Python's OnCompanyCreateAutoSync in chalicelib/bus.py.
// -----------------------------------------------------------------------

// initialCompanyCFDIChunkDays matches POST /api/admin/sat-enqueue default for CFDI requests.
const initialCompanyCFDIChunkDays = 90

// SatSolicitudEnqueueSpacing is the spacing between scheduled enqueue times for SAT
// SolicitaDescarga-bound messages when bootstrapping a company. SAT applies aggressive
// per-RFC limits on concurrent solicitudes; bursts surface as empty IdSolicitud or
// spurious verify errors. See send_query_metadata for METADATA follow-up publishes.
const SatSolicitudEnqueueSpacing = 3 * time.Second

func executeAtForAutosyncSlot(base time.Time, slot int) *time.Time {
	t := base.Add(time.Duration(slot) * SatSolicitudEnqueueSpacing)
	return &t
}

type OnCompanyCreateAutoSync struct {
	Bus *Bus
	Cfg *config.Config
	// TimeNow is optional (tests); production uses time.Now.
	TimeNow func() time.Time
}

func (h *OnCompanyCreateAutoSync) now() time.Time {
	if h != nil && h.TimeNow != nil {
		return h.TimeNow()
	}
	return time.Now()
}

func (h *OnCompanyCreateAutoSync) Handle(ev DomainEvent) error {
	company, ok := ev.(CompanyCreatedEvent)
	if !ok {
		slog.Warn("onCompanyCreateAutoSync: unexpected event type")
		return nil
	}

	start := datetime.LastXFiscalYearsStart(5)

	base := h.now()
	slot := 0
	meta := SQSCompanySendMetadata{
		CompanyBase:       NewCompanyBase(company.CompanyIdentifier, company.CompanyRFC),
		ManuallyTriggered: true,
		WID:               company.WorkspaceID,
		CID:               company.CompanyID,
	}
	meta.ExecuteAt = executeAtForAutosyncSlot(base, slot)
	slot++
	h.Bus.Publish(EventTypeSATMetadataRequested, meta)

	endExclusive := datetime.MXCalendarDate(h.now().In(datetime.MexicoCity())).AddDate(0, 0, 1)
	chunks := datetime.ChunkRangeByDays(start, endExclusive, initialCompanyCFDIChunkDays)
	slog.Warn("company_create_auto_sync",
		"company_identifier", company.CompanyIdentifier,
		"cfdi_chunk_windows", len(chunks),
		"range_start", start.Format(time.RFC3339),
		"range_end_exclusive", endExclusive.Format(time.RFC3339),
		"sat_enqueue_spacing_sec", int(SatSolicitudEnqueueSpacing/time.Second),
		"sat_scheduled_messages", 1+len(chunks)*2,
	)

	for _, ch := range chunks {
		cs, ce := ch.Start, ch.End
		issued := QueryCreateEvent{
			SQSBase:           NewSQSBase(),
			CompanyIdentifier: company.CompanyIdentifier,
			DownloadType:      tenant.DownloadTypeIssued,
			RequestType:       tenant.RequestTypeCFDI,
			IsManual:          false,
			Start:             &cs,
			End:               &ce,
			WID:               company.WorkspaceID,
			CID:               company.CompanyID,
		}
		issued.ExecuteAt = executeAtForAutosyncSlot(base, slot)
		slot++
		h.Bus.Publish(EventTypeSATWSRequestCreateQuery, issued)

		received := QueryCreateEvent{
			SQSBase:           NewSQSBase(),
			CompanyIdentifier: company.CompanyIdentifier,
			DownloadType:      tenant.DownloadTypeReceived,
			RequestType:       tenant.RequestTypeCFDI,
			IsManual:          false,
			Start:             &cs,
			End:               &ce,
			WID:               company.WorkspaceID,
			CID:               company.CompanyID,
		}
		received.ExecuteAt = executeAtForAutosyncSlot(base, slot)
		slot++
		h.Bus.Publish(EventTypeSATWSRequestCreateQuery, received)
	}

	h.Bus.Publish(EventTypeSATCompleteCFDIsNeeded, NeedToCompleteCFDIsEvent{
		CompanyBase:  NewCompanyBase(company.CompanyIdentifier, company.CompanyRFC),
		DownloadType: tenant.DownloadTypeIssued,
		IsManual:     false,
		Start:        &start,
	})
	h.Bus.Publish(EventTypeSATCompleteCFDIsNeeded, NeedToCompleteCFDIsEvent{
		CompanyBase:  NewCompanyBase(company.CompanyIdentifier, company.CompanyRFC),
		DownloadType: tenant.DownloadTypeReceived,
		IsManual:     false,
		Start:        &start,
	})

	// Scraper is skipped in local dev — only webservice runs.
	if h.Cfg.LocalInfra {
		return nil
	}

	// Phase 10 will provide the full scrap request payload.
	h.Bus.Publish(EventTypeRequestScrap, scrapRequestStub{
		CompanyIdentifier: company.CompanyIdentifier,
		CompanyRFC:        company.CompanyRFC,
		WorkspaceID:       company.WorkspaceID,
		CompanyID:         company.CompanyID,
	})
	return nil
}

// -----------------------------------------------------------------------
// requestScrap
// Phase 10 will fully implement the scrap payload building.
// For now it publishes a minimal stub to SQS_SCRAP_ORCHESTRATOR.
// -----------------------------------------------------------------------

type scrapRequestStub struct {
	CompanyIdentifier string `json:"company_identifier"`
	CompanyRFC        string `json:"company_rfc"`
	WorkspaceID       int64  `json:"workspace_id"`
	CompanyID         int64  `json:"company_id"`
}

type RequestScrap struct {
	Bus *Bus
	Cfg *config.Config
}

func (h *RequestScrap) Handle(ev DomainEvent) error {
	// Phase 10: build CompanyScrapEvent from scrap utilities.
	// For now forward the stub payload directly to the orchestrator.
	h.Bus.Publish(EventTypeSQSScrapOrchestrator, ev)
	return nil
}

// -----------------------------------------------------------------------
// onFirstCompanyCreatedRestoreTrial
// Phase 10 will implement the Stripe subscription restore logic.
// -----------------------------------------------------------------------

type OnFirstCompanyCreatedRestoreTrial struct {
	Cfg *config.Config
}

func (h *OnFirstCompanyCreatedRestoreTrial) Handle(ev DomainEvent) error {
	slog.Debug("onFirstCompanyCreatedRestoreTrial: stub — Stripe logic in Phase 10")
	return nil
}

// -----------------------------------------------------------------------
// onQueryReadyToDownloadProcessQuery
// Routes a downloaded SAT WS query to the correct processing queue.
// Mirrors Python's OnQueryReadyToDownloadProcessQuery in chalicelib/bus.py.
// -----------------------------------------------------------------------

// QueryDownloadedEvent is published by the SAT WS download handler after all
// package ZIPs have been stored in blob. Packages are forwarded so that the
// downstream ProcessXML / ProcessMetadata handler knows which ZIPs to fetch.
type QueryDownloadedEvent struct {
	SQSBase
	CompanyIdentifier string     `json:"company_identifier"`
	QueryIdentifier   string     `json:"query_identifier"`
	RequestType       string     `json:"request_type"` // "METADATA" | "CFDI"
	Packages          []string   `json:"packages,omitempty"`
	ExecuteAtOverride *time.Time `json:"execute_at_override,omitempty"`
}

type OnQueryReadyToDownloadProcessQuery struct {
	Bus *Bus
}

func (h *OnQueryReadyToDownloadProcessQuery) Handle(ev DomainEvent) error {
	q, ok := ev.(QueryDownloadedEvent)
	if !ok {
		slog.Warn("onQueryReadyToDownload: unexpected event type")
		return nil
	}

	switch q.RequestType {
	case "METADATA":
		h.Bus.Publish(EventTypeSATMetadataDownloaded, ev)
	case "CFDI":
		h.Bus.Publish(EventTypeSATCFDIsDownloaded, ev)
	default:
		slog.Error("onQueryReadyToDownload: invalid request_type", "type", q.RequestType)
	}
	return nil
}

// -----------------------------------------------------------------------
// queryNeedSplitHandler
// Phase 10 will implement the full split logic.
// -----------------------------------------------------------------------

type QueryNeedSplitHandler struct {
	Bus *Bus
}

func (h *QueryNeedSplitHandler) Handle(ev DomainEvent) error {
	slog.Debug("queryNeedSplitHandler: stub — SAT split logic in Phase 10")
	return nil
}
