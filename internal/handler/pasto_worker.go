package handler

import (
	"encoding/json"
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

type PastoWorker struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
	pasto    *pastoclient.Client
}

func NewPastoWorker(cfg *config.Config, database *db.Database, bus *event.Bus) *PastoWorker {
	pastoClient := pastoclient.NewClient(cfg.PastoURL, cfg.PastoOCPKey, cfg.PastoRequestTimeout)
	return &PastoWorker{cfg: cfg, database: database, bus: bus, pasto: pastoClient}
}

// POST /api/Pasto/Worker/ — create a new ADD worker for a workspace.
// Matches routers/pasto/worker.py::create_worker.
func (h *PastoWorker) CreateWorker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body struct {
		WorkspaceIdentifier string `json:"workspace_identifier"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}
	if body.WorkspaceIdentifier == "" {
		response.BadRequest(w, "workspace_identifier is required")
		return
	}

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var workspace control.Workspace
	if err := database.Primary.NewSelect().
		Model(&workspace).
		Where("identifier = ?", body.WorkspaceIdentifier).
		Limit(1).
		Scan(ctx); err != nil {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "error",
			"message": "Workspace: " + body.WorkspaceIdentifier + " not found",
		})
		return
	}

	if workspace.PastoWorkerID != nil && *workspace.PastoWorkerID != "" {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "error",
			"message": "Workspace: " + body.WorkspaceIdentifier + " already has a worker",
		})
		return
	}

	workerID := "mock_worker_" + body.WorkspaceIdentifier[:8]
	licenseKey := "mock_license_" + body.WorkspaceIdentifier[:8]
	workerToken := "mock_token_" + body.WorkspaceIdentifier[:8]

	if !h.cfg.LocalInfra {
		token, err := h.pasto.Login(h.cfg.PastoEmail, h.cfg.PastoPassword)
		if err != nil {
			slog.Error("pasto_worker: dashboard login failed", "err", err)
			response.InternalError(w, "dashboard login failed")
			return
		}
		worker, err := h.pasto.CreateWorker(token, h.cfg.PastoSubscriptionID, h.cfg.PastoDashboardID, body.WorkspaceIdentifier)
		if err != nil {
			slog.Error("pasto_worker: create_worker failed", "err", err)
			response.InternalError(w, "create worker failed")
			return
		}
		workerID = worker.PastoID
		licenseKey = worker.SerialNumber
		workerToken = worker.Token
	} else {
		slog.Info("pasto_worker: mock mode — using mock worker credentials", "workspace", body.WorkspaceIdentifier)
	}

	_, err = database.Primary.NewUpdate().
		Model((*control.Workspace)(nil)).
		Set("pasto_worker_id = ?", workerID).
		Set("pasto_license_key = ?", licenseKey).
		Set("pasto_worker_token = ?", workerToken).
		Where("identifier = ?", body.WorkspaceIdentifier).
		Exec(ctx)
	if err != nil {
		slog.Error("pasto_worker: update workspace failed", "err", err)
		response.InternalError(w, "update workspace failed")
		return
	}

	h.bus.Publish(event.EventTypePastoWorkerCreated, event.WorkerCreatedEvent{
		SQSBase:             event.NewSQSBase(),
		WorkspaceIdentifier: body.WorkspaceIdentifier,
		WorkerID:            workerID,
		WorkerToken:         workerToken,
	})

	slog.Info("pasto_worker: worker created", "workspace", body.WorkspaceIdentifier, "worker_id", workerID)
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
