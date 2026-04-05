package control

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type Company struct {
	bun.BaseModel `bun:"table:company,alias:c"`

	ID                     int64            `bun:"id,pk,autoincrement" json:"id"`
	Identifier             string           `bun:"identifier,type:uuid,notnull,unique" json:"identifier"`
	Name                   string           `bun:"name,notnull" json:"name"`
	WorkspaceID            *int64           `bun:"workspace_id" json:"workspace_id"`
	WorkspaceIdentifier    *string          `bun:"workspace_identifier,type:uuid" json:"workspace_identifier"`
	RFC                    *string          `bun:"rfc" json:"rfc"`
	Active                 *bool            `bun:"active,default:true" json:"active"`
	HaveCertificates       *bool            `bun:"have_certificates,default:false" json:"have_certificates"`
	HasValidCerts          *bool            `bun:"has_valid_certs,default:false" json:"has_valid_certs"`
	EmailsToSendEfos       json.RawMessage  `bun:"emails_to_send_efos,type:jsonb" json:"emails_to_send_efos"`
	EmailsToSendErrors     json.RawMessage  `bun:"emails_to_send_errors,type:jsonb" json:"emails_to_send_errors"`
	EmailsToSendCanceled   json.RawMessage  `bun:"emails_to_send_canceled,type:jsonb" json:"emails_to_send_canceled"`
	HistoricDownloaded     *bool            `bun:"historic_downloaded,default:false" json:"historic_downloaded"`
	LastWsDownload         *time.Time       `bun:"last_ws_download" json:"last_ws_download"`
	ExceedMetadataLimit    *bool            `bun:"exceed_metadata_limit,notnull" json:"exceed_metadata_limit"`
	PermissionToSync       *bool            `bun:"permission_to_sync,notnull" json:"permission_to_sync"`
	LastNotification       *time.Time       `bun:"last_notification" json:"last_notification"`
	PastoCompanyIdentifier *string          `bun:"pasto_company_identifier,type:uuid" json:"pasto_company_identifier"`
	PastoLastMetadataSync  *time.Time       `bun:"pasto_last_metadata_sync" json:"pasto_last_metadata_sync"`
	ADDAutoSync            *bool            `bun:"add_auto_sync,default:false" json:"add_auto_sync"`
	Data                   json.RawMessage  `bun:"data,type:jsonb,notnull" json:"data"`
	TenantDBName           *string          `bun:"tenant_db_name" json:"tenant_db_name"`
	TenantDBHost           *string          `bun:"tenant_db_host" json:"tenant_db_host"`
	TenantDBPort           *int             `bun:"tenant_db_port,default:5432" json:"tenant_db_port"`
	TenantDBUser           *string          `bun:"tenant_db_user" json:"tenant_db_user"`
	TenantDBPassword       *string          `bun:"tenant_db_password" json:"tenant_db_password"`
	TenantDBSchema         *string          `bun:"tenant_db_schema" json:"tenant_db_schema"`
	CreatedAt              time.Time        `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt              *time.Time       `bun:"updated_at" json:"updated_at"`

	Workspace *Workspace `bun:"rel:belongs-to,join:workspace_identifier=identifier" json:"workspace,omitempty"`
}
