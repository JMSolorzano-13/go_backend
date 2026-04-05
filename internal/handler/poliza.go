package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Poliza struct {
	cfg      *config.Config
	database *db.Database
	files    port.FileStorage
}

func NewPoliza(cfg *config.Config, database *db.Database, files port.FileStorage) *Poliza {
	return &Poliza{cfg: cfg, database: database, files: files}
}

var polizaMeta = crud.ModelMeta{
	DefaultOrderBy: "identifier ASC",
}

func (h *Poliza) Search(w http.ResponseWriter, r *http.Request) {
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

	result, err := crud.Search[tenant.Poliza](ctx, conn, params, polizaMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

func (h *Poliza) CreateMany(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, cid, false)
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
	var body struct {
		Polizas []struct {
			Identifier    string   `json:"identifier"`
			Fecha         string   `json:"fecha"`
			Tipo          string   `json:"tipo"`
			Numero        string   `json:"numero"`
			Concepto      *string  `json:"concepto"`
			SistemaOrigen *string  `json:"sistema_origen"`
			CfdiUUIDs     []string `json:"cfdi_uuids"`
			Movimientos   []struct {
				Identifier     string   `json:"identifier"`
				Numerador      *string  `json:"numerador"`
				CuentaContable *string  `json:"cuenta_contable"`
				Nombre         *string  `json:"nombre"`
				Cargo          float64  `json:"cargo"`
				Abono          float64  `json:"abono"`
				CargoME        float64  `json:"cargo_me"`
				AbonoME        float64  `json:"abono_me"`
				Concepto       *string  `json:"concepto"`
				Referencia     *string  `json:"referencia"`
				TipoDeCambio   *float64 `json:"tipo_de_cambio"`
			} `json:"movimientos"`
		} `json:"polizas"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("begin tx: %v", err))
		return
	}
	defer tx.Rollback()

	var polizas []tenant.Poliza
	var polizaCFDIs []tenant.PolizaCFDI
	var movimientos []tenant.PolizaMovimiento
	toDelete := make([]string, 0, len(body.Polizas))
	type pseudoPK struct{ Fecha, Tipo, Numero string }
	pseudoPKs := make([]pseudoPK, 0, len(body.Polizas))

	now := time.Now().UTC()
	for _, pj := range body.Polizas {
		fecha, parseErr := time.Parse(time.RFC3339, pj.Fecha)
		if parseErr != nil {
			fecha, parseErr = time.Parse("2006-01-02", pj.Fecha)
			if parseErr != nil {
				response.BadRequest(w, fmt.Sprintf("invalid fecha: %s", pj.Fecha))
				return
			}
		}

		if pj.Tipo == "" || pj.Numero == "" {
			continue
		}

		id := pj.Identifier
		if id == "" {
			id = crud.NewIdentifier()
		}

		polizas = append(polizas, tenant.Poliza{
			Identifier:    id,
			Fecha:         fecha,
			Tipo:          pj.Tipo,
			Numero:        pj.Numero,
			Concepto:      pj.Concepto,
			SistemaOrigen: pj.SistemaOrigen,
			CreatedAt:     now,
		})
		toDelete = append(toDelete, id)
		pseudoPKs = append(pseudoPKs, pseudoPK{Fecha: fecha.Format("2006-01-02"), Tipo: pj.Tipo, Numero: pj.Numero})

		for _, uuid := range pj.CfdiUUIDs {
			polizaCFDIs = append(polizaCFDIs, tenant.PolizaCFDI{
				PolizaIdentifier: id,
				UUIDRelated:      uuid,
				CreatedAt:        now,
			})
		}

		for _, mj := range pj.Movimientos {
			mid := mj.Identifier
			if mid == "" {
				mid = crud.NewIdentifier()
			}
			cargoME := mj.CargoME
			abonoME := mj.AbonoME
			if mj.TipoDeCambio != nil && *mj.TipoDeCambio != 0 {
				tc := *mj.TipoDeCambio
				if cargoME == 0 {
					cargoME = mj.Cargo / tc
				}
				if abonoME == 0 {
					abonoME = mj.Abono / tc
				}
			}
			movimientos = append(movimientos, tenant.PolizaMovimiento{
				Identifier:       mid,
				Numerador:        mj.Numerador,
				CuentaContable:   mj.CuentaContable,
				Nombre:           mj.Nombre,
				Cargo:            mj.Cargo,
				Abono:            mj.Abono,
				CargoME:          cargoME,
				AbonoME:          abonoME,
				Concepto:         mj.Concepto,
				Referencia:       mj.Referencia,
				PolizaIdentifier: id,
				CreatedAt:        now,
			})
		}
	}

	if len(toDelete) > 0 {
		_, err = tx.NewDelete().Model((*tenant.PolizaMovimiento)(nil)).
			Where("poliza_identifier IN (?)", bun.In(toDelete)).Exec(ctx)
		if err != nil {
			response.InternalError(w, fmt.Sprintf("delete movimientos: %v", err))
			return
		}
		_, err = tx.NewDelete().Model((*tenant.PolizaCFDI)(nil)).
			Where("poliza_identifier IN (?)", bun.In(toDelete)).Exec(ctx)
		if err != nil {
			response.InternalError(w, fmt.Sprintf("delete cfdi rels: %v", err))
			return
		}
		_, err = tx.NewDelete().Model((*tenant.Poliza)(nil)).
			Where("identifier IN (?)", bun.In(toDelete)).Exec(ctx)
		if err != nil {
			response.InternalError(w, fmt.Sprintf("delete polizas: %v", err))
			return
		}
	}

	if len(pseudoPKs) > 0 {
		checkQ := tx.NewSelect().Model((*tenant.Poliza)(nil))
		for i, pk := range pseudoPKs {
			if i == 0 {
				checkQ = checkQ.Where("(fecha::date = ? AND tipo = ? AND numero = ?)", pk.Fecha, pk.Tipo, pk.Numero)
			} else {
				checkQ = checkQ.WhereOr("(fecha::date = ? AND tipo = ? AND numero = ?)", pk.Fecha, pk.Tipo, pk.Numero)
			}
		}
		var existing []tenant.Poliza
		if err := checkQ.Scan(ctx, &existing); err == nil && len(existing) > 0 {
			msg := "Ya existen las siguientes pólizas (identifier, fecha, tipo, número):\n"
			for _, p := range existing {
				msg += fmt.Sprintf("(`%s`, `%s`, `%s`, `%s`)\n", p.Identifier, p.Fecha.Format("2006-01-02"), p.Tipo, p.Numero)
			}
			response.BadRequest(w, msg)
			return
		}
	}

	if len(polizas) > 0 {
		if _, err = tx.NewInsert().Model(&polizas).Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("insert polizas: %v", err))
			return
		}
	}
	if len(polizaCFDIs) > 0 {
		if _, err = tx.NewInsert().Model(&polizaCFDIs).Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("insert poliza_cfdi: %v", err))
			return
		}
	}
	if len(movimientos) > 0 {
		if _, err = tx.NewInsert().Model(&movimientos).Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("insert movimientos: %v", err))
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.InternalError(w, fmt.Sprintf("commit: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"state":   "success",
		"created": len(polizas),
	})
}

func (h *Poliza) Export(w http.ResponseWriter, r *http.Request) {
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
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	params := crud.ParseSearchBody(body)
	params.Limit = 0 // unlimited

	var records []tenant.Poliza
	dataQ := conn.NewSelect().Model(&records)
	if params.OrderBy == "" {
		params.OrderBy = polizaMeta.DefaultOrderBy
	}
	if err := dataQ.Scan(ctx); err != nil {
		response.InternalError(w, fmt.Sprintf("query: %v", err))
		return
	}

	if len(records) == 0 {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{"url": "EMPTY"})
		return
	}

	exportData, _ := body["export_data"].(map[string]interface{})
	fileName := "polizas_export"
	if exportData != nil {
		if fn, ok := exportData["file_name"].(string); ok {
			fileName = fn
		}
	}

	data := crud.Serialize(records)
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data":       data,
		"file_name":  fileName,
		"export_msg": "full_xlsx_export_deferred_to_phase9",
	})
}
