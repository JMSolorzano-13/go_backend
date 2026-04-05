package tenant

import (
	"time"

	"github.com/uptrace/bun"
)

const (
	ADDSyncStateDraft = "DRAFT"
	ADDSyncStateSent  = "SENT"
	ADDSyncStateError = "ERROR"
)

type ADDSyncRequest struct {
	bun.BaseModel `bun:"table:add_sync_request"`

	Identifier           string     `bun:"identifier,pk,type:uuid" json:"identifier"`
	CreatedAt            time.Time  `bun:"created_at,notnull" json:"created_at"`
	Start                time.Time  `bun:"start,notnull,type:date" json:"start"`
	End                  time.Time  `bun:"end,notnull,type:date" json:"end"`
	XMLsToSend           int64      `bun:"xmls_to_send,notnull" json:"xmls_to_send"`
	XMLsToSendPending    int64      `bun:"xmls_to_send_pending,notnull" json:"xmls_to_send_pending"`
	XMLsToSendTotal      float64    `bun:"xmls_to_send_total,notnull" json:"xmls_to_send_total"`
	CfdisToCancel        int64      `bun:"cfdis_to_cancel,notnull" json:"cfdis_to_cancel"`
	CfdisToCancelPending int64      `bun:"cfdis_to_cancel_pending,notnull" json:"cfdis_to_cancel_pending"`
	CfdisToCancelTotal   float64    `bun:"cfdis_to_cancel_total,notnull" json:"cfdis_to_cancel_total"`
	PastoSentIdentifier  *string    `bun:"pasto_sent_identifier,type:uuid" json:"pasto_sent_identifier"`
	PastoCancelIdentifier *string   `bun:"pasto_cancel_identifier,type:uuid" json:"pasto_cancel_identifier"`
	State                string     `bun:"state,notnull" json:"state"`
	ManuallyTriggered    bool       `bun:"manually_triggered,notnull" json:"manually_triggered"`
	UpdatedAt            *time.Time `bun:"updated_at" json:"updated_at"`
}
