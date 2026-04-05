package tenant

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

const (
	DownloadTypeIssued   = "ISSUED"
	DownloadTypeReceived = "RECEIVED"

	RequestTypeMetadata = "METADATA"
	RequestTypeCFDI     = "CFDI"

	QueryStateDraft      = "DRAFT"
	QueryStateSent       = "SENT"
	QueryStateProcessing = "PROCESSING"
	QueryStateCompleted  = "COMPLETED"
	QueryStateError      = "ERROR"

	SATTechWebService = "WebService"
	SATTechScraper    = "Scraper"
)

type SATQuery struct {
	bun.BaseModel `bun:"table:sat_query"`

	Identifier       string          `bun:"identifier,pk,type:uuid" json:"identifier"`
	Name             string          `bun:"name,notnull" json:"name"`
	Start            time.Time       `bun:"start,notnull" json:"start"`
	End              time.Time       `bun:"end,notnull" json:"end"`
	DownloadType     string          `bun:"download_type,notnull" json:"download_type"`
	RequestType      string          `bun:"request_type,notnull" json:"request_type"`
	Packages         json.RawMessage `bun:"packages,type:json" json:"packages"`
	CfdisQty         *int64          `bun:"cfdis_qty" json:"cfdis_qty"`
	State            string          `bun:"state,notnull" json:"state"`
	SentDate         *time.Time      `bun:"sent_date" json:"sent_date"`
	IsManual         *bool           `bun:"is_manual" json:"is_manual"`
	Technology       string          `bun:"technology,notnull" json:"technology"`
	OriginIdentifier *string         `bun:"origin_identifier,type:uuid" json:"origin_identifier"`
	CreatedAt        time.Time       `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt        *time.Time      `bun:"updated_at" json:"updated_at"`
}
