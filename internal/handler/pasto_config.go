package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	pastoclient "github.com/siigofiscal/go_backend/internal/infra/pasto"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type PastoConfig struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
	pasto    *pastoclient.Client
}

func NewPastoConfig(cfg *config.Config, database *db.Database, bus *event.Bus) *PastoConfig {
	pastoClient := pastoclient.NewClient(cfg.PastoURL, cfg.PastoOCPKey, cfg.PastoRequestTimeout)
	return &PastoConfig{cfg: cfg, database: database, bus: bus, pasto: pastoClient}
}

// POST /api/Pasto/Config — worker configuration webhook from ADD.
// Matches routers/pasto/config.py::config_webhook.
func (h *PastoConfig) ConfigWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

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
	webhookErr, pastoBody, hdrs := parsePastoWebhook(body, headers, "config_webhook")
	if webhookErr {
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	workerID, _ := hdrs["worker_id"].(string)
	if workerID == "" {
		workerID, _ = hdrs["Worker_id"].(string)
	}
	workspaceIdentifier, _ := hdrs["workspace_identifier"].(string)

	dbServer, _ := pastoBody["DbServerName"].(string)
	dbUsername, _ := pastoBody["DbUsername"].(string)
	dbPassword, _ := pastoBody["DbPassword"].(string)

	var workspace control.Workspace
	if err := database.Primary.NewSelect().
		Model(&workspace).
		Where("identifier = ?", workspaceIdentifier).
		Limit(1).
		Scan(ctx); err != nil {
		slog.Error("pasto_config: workspace not found", "workspace", workspaceIdentifier, "err", err)
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if !h.cfg.MockStripe && !h.cfg.LocalInfra {
		token, err := h.pasto.Login(h.cfg.PastoEmail, h.cfg.PastoPassword)
		if err != nil {
			slog.Error("pasto_config: dashboard login failed", "err", err)
			response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if err := h.pasto.SetWorkerCredentials(token, workerID, workspaceIdentifier, dbServer, dbUsername, dbPassword); err != nil {
			slog.Error("pasto_config: set_credentials failed", "err", err)
		}
	} else {
		slog.Info("pasto_config: mock mode — skipping Pasto API calls", "workspace", workspaceIdentifier)
	}

	installed := true
	_, err = database.Primary.NewUpdate().
		Model((*control.Workspace)(nil)).
		Set("pasto_installed = ?", installed).
		Where("identifier = ?", workspaceIdentifier).
		Exec(ctx)
	if err != nil {
		slog.Error("pasto_config: update workspace failed", "err", err)
	}

	workerToken := ""
	if workspace.PastoWorkerToken != nil {
		workerToken = *workspace.PastoWorkerToken
	}

	h.bus.Publish(event.EventTypePastoWorkerCredentialsSet, event.WorkerCredentialsSetEvent{
		SQSBase:             event.NewSQSBase(),
		WorkspaceIdentifier: workspaceIdentifier,
		WorkerID:            workerID,
		WorkerToken:         workerToken,
	})

	slog.Info("pasto_config: credentials set", "workspace", workspaceIdentifier, "worker_id", workerID, "server", dbServer)
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parsePastoWebhook parses the ADD webhook payload.
// Matches helpers/pasto_common.py::parse_pasto_webhook.
// Returns (hasError bool, body map[string]interface{}, headers map).
// The body is a JSON object — use parsePastoWebhookRaw for array bodies.
func parsePastoWebhook(body map[string]interface{}, headers map[string]interface{}, name string) (bool, map[string]interface{}, map[string]interface{}) {
	hasErr, raw, hdrs := parsePastoWebhookRaw(body, headers, name)
	if hasErr || raw == nil {
		return hasErr, nil, hdrs
	}
	if m, ok := raw.(map[string]interface{}); ok {
		return false, m, hdrs
	}
	return false, nil, hdrs
}

// parsePastoWebhookRaw parses the ADD webhook and returns the body as interface{}.
// This handles both JSON object and JSON array bodies.
func parsePastoWebhookRaw(body map[string]interface{}, headers map[string]interface{}, name string) (bool, interface{}, map[string]interface{}) {
	statusRaw, ok := body["Status"]
	if !ok {
		slog.Warn("pasto_webhook: missing Status field", "webhook", name)
		return true, nil, headers
	}
	var statusCode float64
	switch v := statusRaw.(type) {
	case float64:
		statusCode = v
	case int:
		statusCode = float64(v)
	default:
		return true, nil, headers
	}
	if statusCode != 0 {
		slog.Warn("pasto_webhook: non-zero status", "webhook", name, "status", statusCode)
		return true, nil, headers
	}
	rawBody, _ := body["Body"].(string)
	var parsedBody interface{}
	if err := json.Unmarshal([]byte(rawBody), &parsedBody); err != nil {
		slog.Error("pasto_webhook: body parse failed", "webhook", name, "err", err)
		return true, nil, headers
	}
	return false, parsedBody, headers
}

// extractRequestHeaders converts http.Header to a flat map[string]interface{}.
func extractRequestHeaders(r *http.Request) map[string]interface{} {
	m := make(map[string]interface{})
	for k, vals := range r.Header {
		if len(vals) > 0 {
			m[k] = vals[0]
			// Also store lowercase key for easier access
			m[fmt.Sprintf("%s", lowerKey(k))] = vals[0]
		}
	}
	return m
}

func lowerKey(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c + 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}
