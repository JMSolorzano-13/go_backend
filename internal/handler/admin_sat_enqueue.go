package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/domain/datetime"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/response"
)

type AdminSATEnqueue struct {
	cfg *config.Config
	bus *event.Bus
}

func NewAdminSATEnqueue(cfg *config.Config, bus *event.Bus) *AdminSATEnqueue {
	return &AdminSATEnqueue{cfg: cfg, bus: bus}
}

type satEnqueueRequest struct {
	CompanyIdentifier string `json:"company_identifier"`
	WID               int64  `json:"wid"`
	CID               int64  `json:"cid"`
	Start             string `json:"start"`
	End               string `json:"end"`
	RequestType       string `json:"request_type"`
	DownloadType      string `json:"download_type"`
	ChunkDays         int    `json:"chunk_days"`
}

const (
	defaultCFDIChunkDays     = 90
	defaultMetadataChunkDays = 180
)

// Enqueue handles POST /api/admin/sat-enqueue.
// Chunks the date range and publishes SAT_WS_REQUEST_CREATE_QUERY events to the bus.
func (h *AdminSATEnqueue) Enqueue(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var req satEnqueueRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if req.CompanyIdentifier == "" || req.WID == 0 || req.CID == 0 {
		response.BadRequest(w, "company_identifier, wid, and cid are required")
		return
	}
	if req.Start == "" || req.End == "" {
		response.BadRequest(w, "start and end are required (YYYY-MM-DD)")
		return
	}

	startUTC, endUTC, err := datetime.AdminEnqueueCalendarRange(req.Start, req.End)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	reqType := req.RequestType
	if reqType != "CFDI" && reqType != "METADATA" {
		response.BadRequest(w, "request_type must be CFDI or METADATA")
		return
	}

	dlTypes := resolveDownloadTypes(req.DownloadType)
	if len(dlTypes) == 0 {
		response.BadRequest(w, "download_type must be ISSUED, RECEIVED, or BOTH")
		return
	}

	chunkDays := req.ChunkDays
	if chunkDays <= 0 {
		if reqType == "METADATA" {
			chunkDays = defaultMetadataChunkDays
		} else {
			chunkDays = defaultCFDIChunkDays
		}
	}

	chunks := datetime.ChunkRangeByDays(startUTC, endUTC, chunkDays)
	published := 0
	scheduleBase := time.Now().UTC()
	slot := 0
	for _, c := range chunks {
		for _, dl := range dlTypes {
			cs := c.Start
			ce := c.End
			sqs := event.NewSQSBase()
			tExec := scheduleBase.Add(time.Duration(slot) * event.SatSolicitudEnqueueSpacing)
			slot++
			sqs.ExecuteAt = &tExec
			h.bus.Publish(event.EventTypeSATWSRequestCreateQuery, event.QueryCreateEvent{
				SQSBase:           sqs,
				CompanyIdentifier: req.CompanyIdentifier,
				DownloadType:      dl,
				RequestType:       reqType,
				IsManual:          true,
				Start:             &cs,
				End:               &ce,
				WID:               req.WID,
				CID:               req.CID,
			})
			published++
		}
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"published": published,
		"chunks":    len(chunks),
		"details": map[string]interface{}{
			"company_identifier":     req.CompanyIdentifier,
			"request_type":           reqType,
			"download_types":         dlTypes,
			"start":                  req.Start,
			"end":                    req.End,
			"chunk_days":             chunkDays,
			"execute_at_spacing_sec": int(event.SatSolicitudEnqueueSpacing / time.Second),
			"scheduled_messages":     published,
		},
	})
}

func resolveDownloadTypes(dt string) []string {
	switch dt {
	case "ISSUED":
		return []string{"ISSUED"}
	case "RECEIVED":
		return []string{"RECEIVED"}
	case "BOTH":
		return []string{"ISSUED", "RECEIVED"}
	default:
		return nil
	}
}
