package event

import (
	"time"

	"github.com/google/uuid"
)

// DomainEvent is a marker interface — any struct passed to the bus.
type DomainEvent interface{}

// EventHandler is the contract that all bus subscribers must implement.
// The parameter is DomainEvent (interface{}) so implementations in external
// packages (e.g. infra/sqs) can satisfy it without a circular import.
type EventHandler interface {
	Handle(event DomainEvent) error
}

// -----------------------------------------------------------------------
// Base SQS message shapes
// Mirrors the Python SQSMessage / CompanyEvent / SQSCompany hierarchy.
// Field names use snake_case JSON tags to match the Python model_dump_json output.
// -----------------------------------------------------------------------

// SQSBase is the common envelope for all SQS messages.
type SQSBase struct {
	Identifier string     `json:"identifier"`
	ExecuteAt  *time.Time `json:"execute_at"`
}

func NewSQSBase() SQSBase {
	return SQSBase{Identifier: uuid.NewString()}
}

// CompanyBase carries the company context common to most SAT/ADD events.
type CompanyBase struct {
	SQSBase
	CompanyIdentifier string `json:"company_identifier"`
	CompanyRFC        string `json:"company_rfc"`
}

func NewCompanyBase(companyIdentifier, companyRFC string) CompanyBase {
	return CompanyBase{
		SQSBase:           NewSQSBase(),
		CompanyIdentifier: companyIdentifier,
		CompanyRFC:        companyRFC,
	}
}

// -----------------------------------------------------------------------
// Concrete event types published by the complex handlers
// -----------------------------------------------------------------------

// SQSCompanySendMetadata is published for EventTypeSATMetadataRequested.
type SQSCompanySendMetadata struct {
	CompanyBase
	ManuallyTriggered bool  `json:"manually_triggered"`
	WID               int64 `json:"wid"`
	CID               int64 `json:"cid"`
}

// NeedToCompleteCFDIsEvent is published for EventTypeSATCompleteCFDIsNeeded.
type NeedToCompleteCFDIsEvent struct {
	CompanyBase
	DownloadType string     `json:"download_type"`
	IsManual     bool       `json:"is_manual"`
	Start        *time.Time `json:"start"`
	End          *time.Time `json:"end"`
}

// QueryCreateEvent mirrors Python's QueryCreateEvent for SAT_WS_REQUEST_CREATE_QUERY.
type QueryCreateEvent struct {
	SQSBase
	CompanyIdentifier string     `json:"company_identifier"`
	DownloadType      string     `json:"download_type"`
	RequestType       string     `json:"request_type"`
	IsManual          bool       `json:"is_manual"`
	Start             *time.Time `json:"start"`
	End               *time.Time `json:"end"`
	WID               int64      `json:"wid"`
	CID               int64      `json:"cid"`
}

// QueryVerifyEvent mirrors the internal Query object published for reverification.
type QueryVerifyEvent struct {
	SQSBase
	CompanyIdentifier string    `json:"company_identifier"`
	QueryIdentifier   string    `json:"query_identifier"`
	DownloadType      string    `json:"download_type"`
	RequestType       string    `json:"request_type"`
	Start             time.Time `json:"start"`
	End               time.Time `json:"end"`
	State             string    `json:"state"`
	Name              string    `json:"name"`
	SentDate          time.Time `json:"sent_date"`
	IsManual          bool      `json:"is_manual"`
	WID               int64     `json:"wid"`
	CID               int64     `json:"cid"`
}

// ScrapRequestEvent is published for EventTypeRequestScrap from manual/massive endpoints.
type ScrapRequestEvent struct {
	CompanyIdentifier string `json:"company_identifier"`
	CompanyRFC        string `json:"company_rfc"`
	WorkspaceID       int64  `json:"workspace_id"`
	CompanyID         int64  `json:"company_id"`
}

// SQSMessagePayload is a generic JSON payload published to SQS.
type SQSMessagePayload struct {
	SQSBase
	CompanyIdentifier string                 `json:"company_identifier"`
	JSONBody          map[string]interface{} `json:"json_body,omitempty"`
}

// SQSCompany is the base event carrying only company_identifier.
// Matches Python SQSCompany(SQSMessage, CompanyEvent).
type SQSCompany struct {
	SQSBase
	CompanyIdentifier string `json:"company_identifier"`
}

// SQSCompanyManual adds manually_triggered to SQSCompany.
type SQSCompanyManual struct {
	SQSCompany
	ManuallyTriggered bool `json:"manually_triggered"`
}

// -----------------------------------------------------------------------
// Pasto/ADD event types
// -----------------------------------------------------------------------

// WorkerCreatedEvent is published for EventTypePastoWorkerCreated.
type WorkerCreatedEvent struct {
	SQSBase
	WorkspaceIdentifier string `json:"workspace_identifier"`
	WorkerID            string `json:"worker_id"`
	WorkerToken         string `json:"worker_token"`
}

// WorkerCredentialsSetEvent is published for EventTypePastoWorkerCredentialsSet.
type WorkerCredentialsSetEvent struct {
	SQSBase
	WorkspaceIdentifier string `json:"workspace_identifier"`
	WorkerID            string `json:"worker_id"`
	WorkerToken         string `json:"worker_token"`
}

// ADDSyncRequestCreatedEvent is published for EventTypeADDSyncRequestCreated.
// Matches Python ADDDataSync.
type ADDSyncRequestCreatedEvent struct {
	SQSBase
	CompanyIdentifier      string `json:"company_identifier"`
	RequestIdentifier      string `json:"request_identifier"`
	PastoCompanyIdentifier string `json:"pasto_company_identifier"`
	PastoWorkerToken       string `json:"pasto_worker_token"`
	Start                  string `json:"start"`
	End                    string `json:"end"`
}

// ADDResetLicenseKeyEvent is published for EventTypePastoResetLicenseKeyRequested.
type ADDResetLicenseKeyEvent struct {
	SQSBase
	LicenseKey string `json:"license_key"`
}

// COIMetadataUploadedEvent is published for EventTypeCOIMetadataUploaded.
// Matches Python COIMetadataUploaded.
type COIMetadataUploadedEvent struct {
	SQSBase
	CompanyIdentifier string `json:"company_identifier"`
	RequestIdentifier string `json:"request_identifier"`
	LaunchSync        bool   `json:"launch_sync"`
}
