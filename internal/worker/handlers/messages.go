package handlers

import (
	"encoding/json"
	"time"
)

// CreateQueryMsg is the JSON body received from the queue-create-query queue.
// Matches event.QueryCreateEvent published by the API.
type CreateQueryMsg struct {
	Identifier        string     `json:"identifier"`
	CompanyIdentifier string     `json:"company_identifier"`
	DownloadType      string     `json:"download_type"`
	RequestType       string     `json:"request_type"`
	IsManual          bool       `json:"is_manual"`
	Start             *time.Time `json:"start"`
	End               *time.Time `json:"end"`
	WID               int64      `json:"wid"`
	CID               int64      `json:"cid"`
}

// VerifyQueryMsg is the JSON body received from data-queue-verify-request.
// This is the Query object as serialized by the sender/verify loop.
type VerifyQueryMsg struct {
	Identifier        string    `json:"identifier"`
	CompanyIdentifier string    `json:"company_identifier"`
	QueryIdentifier   string    `json:"query_identifier"`
	DownloadType      string    `json:"download_type"`
	RequestType       string    `json:"request_type"`
	Start             time.Time `json:"start"`
	End               time.Time `json:"end"`
	State             string    `json:"state"`
	Name              string    `json:"name"` // SAT solicitud UUID
	IsManual          bool      `json:"is_manual"`
	SentDate          time.Time `json:"sent_date"`
	WID               int64     `json:"wid"`
	CID               int64     `json:"cid"`
}

// DownloadQueryMsg is the JSON body received from data-queue-download-zips-s3.
// Sent after verify confirms packages are ready.
type DownloadQueryMsg struct {
	Identifier        string    `json:"identifier"`
	CompanyIdentifier string    `json:"company_identifier"`
	QueryIdentifier   string    `json:"query_identifier"`
	DownloadType      string    `json:"download_type"`
	RequestType       string    `json:"request_type"`
	Start             time.Time `json:"start"`
	End               time.Time `json:"end"`
	State             string    `json:"state"`
	Name              string    `json:"name"`
	IsManual          bool      `json:"is_manual"`
	CfdisQty          int64     `json:"cfdis_qty"`
	Packages          []string  `json:"packages"`
	WID               int64     `json:"wid"`
	CID               int64     `json:"cid"`
}

// ProcessQueryMsg is the JSON body received from data-queue-metadata and
// queue-process-xml-query. Routed by OnQueryReadyToDownloadProcessQuery.
type ProcessQueryMsg struct {
	Identifier        string    `json:"identifier"`
	CompanyIdentifier string    `json:"company_identifier"`
	QueryIdentifier   string    `json:"query_identifier"`
	DownloadType      string    `json:"download_type"`
	RequestType       string    `json:"request_type"`
	Start             time.Time `json:"start"`
	End               time.Time `json:"end"`
	Name              string    `json:"name"`
	IsManual          bool      `json:"is_manual"`
	CfdisQty          int64     `json:"cfdis_qty"`
	Packages          []string  `json:"packages"`
	WID               int64     `json:"wid"`
	CID               int64     `json:"cid"`
}

// CompleteCFDIsMsg is the JSON body received from queue-complete-cfdi.
// Published by the metadata processor after processing.
type CompleteCFDIsMsg struct {
	Identifier        string     `json:"identifier"`
	CompanyIdentifier string     `json:"company_identifier"`
	CompanyRFC        string     `json:"company_rfc"`
	DownloadType      string     `json:"download_type"`
	IsManual          bool       `json:"is_manual"`
	Start             *time.Time `json:"start"`
	End               *time.Time `json:"end"`
}

// UpdaterMsg is the JSON body published to the WS_UPDATER queue.
// Centralizes all SAT query DB state updates.
type UpdaterMsg struct {
	Identifier        string          `json:"identifier"`
	QueryIdentifier   string          `json:"query_identifier"`
	CompanyIdentifier string          `json:"company_identifier"`
	RequestType       string          `json:"request_type"`
	State             string          `json:"state"`
	StateUpdateAt     time.Time       `json:"state_update_at"`
	CfdisQty          *int64          `json:"cfdis_qty,omitempty"`
	Packages          json.RawMessage `json:"packages,omitempty"`
	Name              string          `json:"name,omitempty"`
	SentDate          *time.Time      `json:"sent_date,omitempty"`
}
