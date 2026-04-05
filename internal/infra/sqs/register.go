package sqs

import (
	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
)

// sqsAdapter bridges *Handler (interface{}) to event.EventHandler.
type sqsAdapter struct {
	h *Handler
}

func (a *sqsAdapter) Handle(ev event.DomainEvent) error { return a.h.Handle(ev) }

// SubscribeAllHandlers registers all SQS-backed and domain handlers on the bus.
// Mirrors Python's suscribe_all_handlers() in chalicelib/bus.py.
func SubscribeAllHandlers(bus *event.Bus, cfg *config.Config, pub port.MessagePublisher) {
	sqsH := func(queueURL string) event.EventHandler {
		return &sqsAdapter{h: NewHandler(queueURL, pub)}
	}

	// SAT
	bus.Subscribe(event.EventTypeSATMetadataRequested, sqsH(cfg.SQSSendQueryMetadata))
	bus.Subscribe(event.EventTypeSATMetadataDownloaded, sqsH(cfg.SQSProcessPackageMetadata))
	bus.Subscribe(event.EventTypeSATWSQuerySent, sqsH(cfg.SQSVerifyQuery))
	bus.Subscribe(event.EventTypeSATWSQueryVerifyNeeded, sqsH(cfg.SQSVerifyQuery))
	bus.Subscribe(event.EventTypeSATWSQueryDownloadReady, sqsH(cfg.SQSDownloadQuery))
	bus.Subscribe(event.EventTypeWSUpdater, sqsH(cfg.SQSUpdaterQuery))
	bus.Subscribe(event.EventTypeSATWSRequestCreateQuery, sqsH(cfg.SQSCreateQuery))
	bus.Subscribe(event.EventTypeSATCFDIsDownloaded, sqsH(cfg.SQSProcessPackageXML))
	bus.Subscribe(event.EventTypeSATCFDIsProcessDelayed, sqsH(cfg.SQSProcessPackageXML))
	bus.Subscribe(event.EventTypeSATCompleteCFDIsNeeded, sqsH(cfg.SQSCompleteCFDIs))
	bus.Subscribe(event.EventTypeSQSScrapOrchestrator, sqsH(cfg.SQSScrapOrchestrator))
	bus.Subscribe(event.EventTypeSQSScrapDelay, sqsH(cfg.SQSScrapDelayer))
	// ADD
	bus.Subscribe(event.EventTypeADDMetadataRequested, sqsH(cfg.SQSADDMetadataRequest))
	bus.Subscribe(event.EventTypeADDMetadataDownloaded, sqsH(cfg.SQSADDProcessMetadata))
	bus.Subscribe(event.EventTypeADDSyncRequestCreated, sqsH(cfg.SQSADDDataSync))
	// PASTO
	bus.Subscribe(event.EventTypePastoWorkerCreated, sqsH(cfg.SQSPastoConfigWorker))
	bus.Subscribe(event.EventTypePastoWorkerCredentialsSet, sqsH(cfg.SQSPastoGetCompanies))
	bus.Subscribe(event.EventTypePastoResetLicenseKeyRequested, sqsH(cfg.SQSResetADDLicenseKey))
	// EXPORT
	bus.Subscribe(event.EventTypeUserExportCreated, sqsH(cfg.SQSExport))
	bus.Subscribe(event.EventTypeMassiveExportCreated, sqsH(cfg.SQSMassiveExport))
	// SCRAPER
	bus.Subscribe(event.EventTypeSATScrapPDF, sqsH(cfg.SQSSATScrapPDF))
	// NOTIFICATIONS
	bus.Subscribe(event.EventTypeNotifications, sqsH(cfg.SQSNotifications))
	// COI
	bus.Subscribe(event.EventTypeCOIMetadataUploaded, sqsH(cfg.SQSCOIMetadataUploaded))
	bus.Subscribe(event.EventTypeCOISyncData, sqsH(cfg.SQSCOIDataSync))

	// Complex domain handlers
	bus.Subscribe(event.EventTypeCompanyCreated, &event.OnCompanyCreateAutoSync{Bus: bus, Cfg: cfg})
	bus.Subscribe(event.EventTypeRequestScrap, &event.RequestScrap{Bus: bus, Cfg: cfg})
	bus.Subscribe(event.EventTypeRequestRestoreTrial, &event.OnFirstCompanyCreatedRestoreTrial{Cfg: cfg})
	bus.Subscribe(event.EventTypeSATWSQueryDownloaded, &event.OnQueryReadyToDownloadProcessQuery{Bus: bus})
	bus.Subscribe(event.EventTypeSATSplitNeeded, &event.QueryNeedSplitHandler{Bus: bus})

	event.LogRegistrations(bus)
}
