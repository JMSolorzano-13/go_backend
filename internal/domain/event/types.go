package event

// EventType matches chalicelib/new/shared/domain/event/event_type.py
type EventType string

const (
	// Company
	EventTypeCompanyCreated        EventType = "COMPANY_CREATED"
	EventTypeRequestRestoreTrial   EventType = "REQUEST_RESTORE_TRIAL"
	// SAT
	EventTypeSATMetadataRequested      EventType = "SAT_METADATA_REQUESTED"
	EventTypeSATMetadataDownloaded     EventType = "SAT_METADATA_DOWNLOADED"
	EventTypeWSUpdater                 EventType = "WS_UPDATER"
	EventTypeSATMetadataProcessed      EventType = "SAT_METADATA_PROCESSED"
	EventTypeSATCFDIsDownloaded        EventType = "SAT_CFDIS_DOWNLOADED"
	EventTypeSATCFDIsProcessDelayed    EventType = "SAT_CFDIS_PROCESS_DELAYED"
	EventTypeSATWSQueryDownloaded      EventType = "SAT_WS_QUERY_DOWNLOADED"
	EventTypeSATScrapNeeded            EventType = "SAT_SCRAP_NEEDED"
	EventTypeSATSplitNeeded            EventType = "SAT_SPLIT_NEEDED"
	EventTypeSATWSQueryDownloadReady   EventType = "SAT_WS_QUERY_DOWNLOAD_READY"
	EventTypeSATWSQueryVerifyNeeded    EventType = "SAT_WS_QUERY_VERIFY_NEEDED"
	EventTypeSATWSQuerySent            EventType = "SAT_WS_QUERY_SENT"
	EventTypeSATWSRequestCreateQuery   EventType = "SAT_WS_REQUEST_CREATE_QUERY"
	EventTypeSATCompleteCFDIsNeeded    EventType = "SAT_COMPLETE_CFDIS_NEEDED"
	EventTypeSATCompleteCFDIsScrapNeeded EventType = "SAT_COMPLETE_CFDIS_SCRAP_NEEDED"
	EventTypeSQSScrapOrchestrator      EventType = "SQS_SCRAP_ORCHESTRATOR"
	// ADD
	EventTypeADDMetadataRequested  EventType = "ADD_METADATA_REQUESTED"
	EventTypeADDMetadataDownloaded EventType = "ADD_METADATA_DOWNLOADED"
	EventTypeADDSyncRequestCreated EventType = "ADD_SYNC_REQUEST_CREATED"
	// PASTO
	EventTypePastoResetLicenseKeyRequested EventType = "PASTO_RESET_LICENSE_KEY_REQUESTED"
	EventTypePastoWorkerCreated            EventType = "PASTO_WORKER_CREATED"
	EventTypePastoWorkerCredentialsSet     EventType = "PASTO_WORKER_CREDENTIALS_SET"
	// EXPORT
	EventTypeUserExportCreated    EventType = "USER_EXPORT_CREATED"
	EventTypeMassiveExportCreated EventType = "MASSIVE_EXPORT_CREATED"
	// SCRAPER
	EventTypeSATScrapPDF EventType = "SAT_SCRAP_PDF"
	EventTypeSQSScrapDelay EventType = "SQS_SCRAP_DELAY"
	EventTypeRequestScrap  EventType = "REQUEST_SCRAP"
	// NOTIFICATIONS
	EventTypeNotifications EventType = "NOTIFICATIONS"
	// COI
	EventTypeCOIMetadataUploaded EventType = "COI_METADATA_UPLOADED"
	EventTypeCOISyncData         EventType = "COI_SYNC_DATA"
)
