package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/response"
)

const appVersion = "40.0.0"

type Status struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
}

func NewStatus(cfg *config.Config, database *db.Database, bus *event.Bus) *Status {
	return &Status{cfg: cfg, database: database, bus: bus}
}

func (s *Status) HealthAPI(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Status) HealthDB(w http.ResponseWriter, r *http.Request) {
	if err := s.database.Ping(r.Context()); err != nil {
		response.WriteError(w, "ChaliceViewError", err.Error(), http.StatusInternalServerError)
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Status) Version(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, map[string]string{"version": appVersion})
}

// SQSTest publishes a test event to the EventBus → SQS.
// POST /api/status/sqs-test with {"event_type":"SAT_METADATA_REQUESTED"} or any EventType.
func (s *Status) SQSTest(w http.ResponseWriter, r *http.Request) {
	if s.bus == nil {
		response.InternalError(w, "event bus not initialized")
		return
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "failed to read body")
		return
	}
	defer r.Body.Close()

	var body struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.EventType == "" {
		response.BadRequest(w, `expected {"event_type":"SAT_METADATA_REQUESTED"}`)
		return
	}

	msg := event.SQSCompanySendMetadata{
		CompanyBase:       event.NewCompanyBase("test-company-uuid", "TEST010101AAA"),
		ManuallyTriggered: true,
		WID:               1,
		CID:               1,
	}

	s.bus.Publish(event.EventType(body.EventType), msg)
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"published":  true,
		"event_type": body.EventType,
		"message":    msg,
	})
}

