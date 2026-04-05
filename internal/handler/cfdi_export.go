package handler

import (
	"fmt"
	"io"
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

type CfdiExport struct {
	cfg      *config.Config
	database *db.Database
}

func NewCfdiExport(cfg *config.Config, database *db.Database) *CfdiExport {
	return &CfdiExport{cfg: cfg, database: database}
}

var cfdiExportMeta = crud.ModelMeta{
	DefaultOrderBy: "created_at DESC",
}

func (h *CfdiExport) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, cid, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	params, _, err := crud.ParseSearchBodyJSON(raw)
	if err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	result, err := crud.Search[tenant.CfdiExport](ctx, conn, params, cfdiExportMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}
