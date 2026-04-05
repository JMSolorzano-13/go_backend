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
	"github.com/siigofiscal/go_backend/internal/response"
)

type PastoReset struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
	pasto    *pastoclient.Client
}

func NewPastoReset(cfg *config.Config, database *db.Database, bus *event.Bus) *PastoReset {
	pastoClient := pastoclient.NewClient(cfg.PastoURL, cfg.PastoOCPKey, cfg.PastoRequestTimeout)
	return &PastoReset{cfg: cfg, database: database, bus: bus, pasto: pastoClient}
}

// POST /api/Pasto/ResetLicense/ — reset ADD license via dashboard.
// Matches routers/pasto/reset.py::reset_license.
func (h *PastoReset) ResetLicense(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}
	var body struct {
		LicenseKey string `json:"license_key"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}
	if body.LicenseKey == "" {
		response.BadRequest(w, "license_key is required")
		return
	}

	if h.cfg.LocalInfra {
		slog.Info("pasto_reset: mock mode — skipping Pasto API", "license_key", body.LicenseKey)
		h.bus.Publish(event.EventTypePastoResetLicenseKeyRequested, event.ADDResetLicenseKeyEvent{
			SQSBase:    event.NewSQSBase(),
			LicenseKey: body.LicenseKey,
		})
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	token, err := h.pasto.Login(h.cfg.PastoEmail, h.cfg.PastoPassword)
	if err != nil {
		slog.Error("pasto_reset: dashboard login failed", "err", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"status":  "error",
			"message": "Ocurrio un error al resetear la licencia",
		})
		return
	}

	statusCode, err := h.pasto.ResetLicense(token, body.LicenseKey)
	if err != nil || statusCode != 200 {
		slog.Error("pasto_reset: reset failed", "status", statusCode, "err", err)
		code := http.StatusInternalServerError
		if statusCode > 0 {
			code = statusCode
		}
		response.WriteJSON(w, code, map[string]interface{}{
			"status":  "error",
			"message": "Ocurrio un error al resetear la licencia",
		})
		return
	}

	h.bus.Publish(event.EventTypePastoResetLicenseKeyRequested, event.ADDResetLicenseKeyEvent{
		SQSBase:    event.NewSQSBase(),
		LicenseKey: body.LicenseKey,
	})

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
