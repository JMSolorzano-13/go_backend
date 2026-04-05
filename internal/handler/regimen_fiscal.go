package handler

import (
	"fmt"
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type RegimenFiscal struct {
	cfg      *config.Config
	database *db.Database
}

func NewRegimenFiscal(cfg *config.Config, database *db.Database) *RegimenFiscal {
	return &RegimenFiscal{cfg: cfg, database: database}
}

// GetAll handles GET /api/RegimenFiscal/ — returns fiscal regimes from the catalog table.
//
// Python source calls Odoo XML-RPC (RegimenFiscalRetriever.get_all) and returns {odoo_id: name}.
// Until the Odoo client is implemented (Phase 11), we serve from cat_regimen_fiscal which
// contains the same data loaded via DB migrations, returning {code: name} to match the
// catalog format used locally.
func (h *RegimenFiscal) GetAll(w http.ResponseWriter, r *http.Request) {
	var regimes []control.CatRegimenFiscal
	if err := h.database.Replica.NewSelect().
		Model(&regimes).
		OrderExpr("code ASC").
		Scan(r.Context()); err != nil {
		response.InternalError(w, fmt.Sprintf("query regimen fiscal: %v", err))
		return
	}

	result := make(map[string]string, len(regimes))
	for _, r := range regimes {
		result[r.Code] = r.Name
	}

	response.WriteJSON(w, http.StatusOK, result)
}
