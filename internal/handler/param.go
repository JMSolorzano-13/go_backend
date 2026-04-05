package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Param struct {
	cfg      *config.Config
	database *db.Database
}

func NewParam(cfg *config.Config, database *db.Database) *Param {
	return &Param{cfg: cfg, database: database}
}

var paramMeta = crud.ModelMeta{
	DefaultOrderBy: "name ASC",
	FuzzyFields:    []string{"name", "value"},
}

// Search handles POST /api/Param/search — no auth required (read-only catalog).
func (h *Param) Search(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var rawBody map[string]interface{}
	if err := json.Unmarshal(body, &rawBody); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	params := crud.ParseSearchBody(rawBody)
	result, err := crud.Search[control.Param](r.Context(), h.database.Replica, params, paramMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, result)
}
