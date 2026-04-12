package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// insertCfdiRelaciones creates rows in the cfdi_relation table for every
// (uuid_origin, uuid_related, TipoRelacion) triple found in the parsed
// CfdiRelacionados JSON field.
//
// Mirrors Python XMLProcessor.generate_cfdi_relations.
// Uses deterministic UUIDs so repeated processing is idempotent.
// Errors are logged and never fail the parent CFDI processing.
func (h *ProcessXML) insertCfdiRelaciones(
	ctx context.Context,
	conn bun.Conn,
	data *cfdiXMLData,
	companyID string,
	now time.Time,
	logger *slog.Logger,
) {
	if data.CfdiRelacionados == "" {
		return
	}

	// CfdiRelacionados is serialized from Go structs (not xmltodict), so field names
	// use Go CamelCase: TipoRelacion, CfdiRelacionado, UUID.
	var rels []struct {
		TipoRelacion    string `json:"TipoRelacion"`
		CfdiRelacionado []struct {
			UUID string `json:"UUID"`
		} `json:"CfdiRelacionado"`
	}
	if err := json.Unmarshal([]byte(data.CfdiRelacionados), &rels); err != nil {
		logger.Debug("parse CfdiRelacionados JSON failed", "uuid", data.UUID, "error", err)
		return
	}

	var rows []tenant.CfdiRelacionado
	for _, rel := range rels {
		tipoRelacion := strings.TrimSpace(rel.TipoRelacion)
		for _, r := range rel.CfdiRelacionado {
			uuidRelated := strings.ToLower(strings.TrimSpace(r.UUID))
			if uuidRelated == "" {
				continue
			}
			// Deterministic ID: reprocessing the same CFDI produces the same row identifier.
			id := uuid.NewSHA1(uuid.NameSpaceURL,
				[]byte(fmt.Sprintf("cfdirel:%s:%s:%s:%s", companyID, data.UUID, uuidRelated, tipoRelacion)),
			).String()

			rows = append(rows, tenant.CfdiRelacionado{
				Identifier:        id,
				CompanyIdentifier: companyID,
				IsIssued:          data.IsIssued,
				UUIDOrigin:        data.UUID,
				TipoDeComprobante: data.TipoDeComprobante,
				Estatus:           true,
				UUIDRelated:       uuidRelated,
				TipoRelacion:      tipoRelacion,
				CreatedAt:         &now,
				UpdatedAt:         &now,
			})
		}
	}

	if len(rows) == 0 {
		return
	}

	// Upsert: update Estatus and TipoDeComprobante if already exists
	// (matches Python's update_multiple logic).
	if _, err := conn.NewInsert().
		Model(&rows).
		On(`CONFLICT (identifier, company_identifier, is_issued) DO UPDATE`).
		Set(`"Estatus" = EXCLUDED."Estatus"`).
		Set(`"TipoDeComprobante" = EXCLUDED."TipoDeComprobante"`).
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx); err != nil {
		logger.Error("insert cfdi_relation failed", "uuid", data.UUID, "count", len(rows), "error", err)
	}
}
