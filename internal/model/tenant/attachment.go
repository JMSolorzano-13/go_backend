package tenant

import (
	"time"

	"github.com/uptrace/bun"
)

const (
	AttachmentStatePending   = "PENDING"
	AttachmentStateConfirmed = "CONFIRMED"
	AttachmentStateDeleted   = "DELETED"
)

type Attachment struct {
	bun.BaseModel `bun:"table:attachment"`

	Identifier        string     `bun:"identifier,pk,type:uuid" json:"identifier"`
	CfdiUUID          string     `bun:"cfdi_uuid,notnull,type:uuid" json:"cfdi_uuid"`
	CreatorIdentifier string     `bun:"creator_identifier,notnull,type:uuid" json:"creator_identifier"`
	DeleterIdentifier *string    `bun:"deleter_identifier,type:uuid" json:"deleter_identifier"`
	DeletedAt         *time.Time `bun:"deleted_at" json:"deleted_at"`
	Size              int64      `bun:"size,notnull" json:"size"`
	FileName          string     `bun:"file_name,notnull" json:"file_name"`
	ContentHash       string     `bun:"content_hash,notnull" json:"content_hash"`
	S3Key             string     `bun:"s3_key,notnull" json:"s3_key"`
	State             string     `bun:"state,notnull" json:"state"`
	CreatedAt         time.Time  `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt         *time.Time `bun:"updated_at" json:"updated_at"`
}
