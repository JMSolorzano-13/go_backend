package tenant

import (
	"time"

	"github.com/uptrace/bun"
)

const (
	ExportStateSent       = "SENT"
	ExportStateToDownload = "TO_DOWNLOAD"
	ExportStateError      = "ERROR"

	ExportDataTypeCFDI = "CFDI"
	ExportDataTypeIVA  = "IVA"
	ExportDataTypeISR  = "ISR"
)

type CfdiExport struct {
	bun.BaseModel `bun:"table:cfdi_export"`

	Identifier      string     `bun:"identifier,pk,type:uuid" json:"identifier"`
	URL             *string    `bun:"url" json:"url"`
	State           *string    `bun:"state" json:"state"`
	ExpirationDate  *time.Time `bun:"expiration_date" json:"expiration_date"`
	Start           *string    `bun:"start" json:"start"`
	End             *string    `bun:"end" json:"end"`
	CfdiType        *string    `bun:"cfdi_type" json:"cfdi_type"`
	DownloadType    *string    `bun:"download_type" json:"download_type"`
	Format          *string    `bun:"format" json:"format"`
	ExternalRequest *bool      `bun:"external_request,default:false" json:"external_request"`
	ExportDataType  *string    `bun:"export_data_type" json:"export_data_type"`
	DisplayedName   string     `bun:"displayed_name,notnull" json:"displayed_name"`
	FileName        string     `bun:"file_name,notnull" json:"file_name"`
	Domain          *string    `bun:"domain" json:"domain"`
	CreatedAt       time.Time  `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt       *time.Time `bun:"updated_at" json:"updated_at"`
}
