package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	dtdomain "github.com/siigofiscal/go_backend/internal/domain/datetime"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

type PastoMetadata struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
}

func NewPastoMetadata(cfg *config.Config, database *db.Database, bus *event.Bus) *PastoMetadata {
	return &PastoMetadata{cfg: cfg, database: database, bus: bus}
}

// POST /api/Pasto/Metadata — metadata download notification from ADD.
// Matches routers/pasto/metadata.py::metadata_webhook.
func (h *PastoMetadata) MetadataWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	headers := extractRequestHeaders(r)
	webhookErr, _, hdrs := parsePastoWebhook(body, headers, "metadata_webhook")

	companyIdentifier := headerStr(hdrs, "company_identifier")

	if webhookErr {
		if companyIdentifier != "" {
			database := db.FromContext(ctx)
			if database == nil {
				database = h.database
			}
			tenantConn, err := database.TenantConn(ctx, companyIdentifier, false)
			if err == nil {
				defer tenantConn.Close()
				now := time.Now().UTC()
				n := time.Now().In(dtdomain.MexicoCity())
				today := dtdomain.MXCalendarDate(n)
				monthStart := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC)
				req := &tenant.ADDSyncRequest{
					Identifier:        uuid.NewString(),
					CreatedAt:         now,
					Start:             monthStart,
					End:               today,
					ManuallyTriggered: false,
					State:             tenant.ADDSyncStateError,
				}
				_, _ = tenantConn.NewInsert().Model(req).Exec(ctx)
			}
		}
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	slog.Info("pasto_metadata: publishing ADD_METADATA_DOWNLOADED", "company", companyIdentifier)

	h.bus.Publish(event.EventTypeADDMetadataDownloaded, event.SQSCompany{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: companyIdentifier,
	})

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// headerStr extracts a string value from the flat headers map using lowercase key.
func headerStr(hdrs map[string]interface{}, key string) string {
	if v, ok := hdrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	// Try canonical form (first letter caps)
	if len(key) > 0 {
		canonical := string([]byte{key[0] - 32}) + key[1:]
		if v, ok := hdrs[canonical]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
