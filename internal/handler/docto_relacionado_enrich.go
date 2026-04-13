package handler

import (
	"context"
	"strings"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
)

// enrichDoctoRelacionadoNested adds cfdi_related, cfdi_origin, and payment_related
// for DoctoRelacionado search (Python nested ORM shape).
func enrichDoctoRelacionadoNested(ctx context.Context, conn bun.Conn, companyID string, result *crud.SearchResult) {
	if result == nil || len(result.Data) == 0 {
		return
	}

	relatedUUIDs := make(map[string]struct{})
	originUUIDs := make(map[string]struct{})
	payIDs := make(map[string]struct{})

	for _, rec := range result.Data {
		if ur, ok := rec["UUID_related"].(string); ok && strings.TrimSpace(ur) != "" {
			u := strings.ToLower(strings.TrimSpace(ur))
			relatedUUIDs[u] = struct{}{}
		}
		if uo, ok := rec["UUID"].(string); ok && strings.TrimSpace(uo) != "" {
			u := strings.ToLower(strings.TrimSpace(uo))
			originUUIDs[u] = struct{}{}
		}
		if pid, ok := rec["payment_identifier"].(string); ok && strings.TrimSpace(pid) != "" {
			payIDs[strings.TrimSpace(pid)] = struct{}{}
		}
	}

	allUUIDs := make(map[string]struct{})
	for u := range relatedUUIDs {
		allUUIDs[u] = struct{}{}
	}
	for u := range originUUIDs {
		allUUIDs[u] = struct{}{}
	}
	uuidList := make([]string, 0, len(allUUIDs))
	for u := range allUUIDs {
		uuidList = append(uuidList, u)
	}

	cfdiByUUID := make(map[string][]tenant.CFDI)
	if len(uuidList) > 0 {
		var cfdis []tenant.CFDI
		if err := conn.NewSelect().Model(&cfdis).
			Where("company_identifier = ?", companyID).
			Where(`"UUID" IN (?)`, bun.In(uuidList)).
			Scan(ctx); err == nil {
			for _, c := range cfdis {
				u := strings.ToLower(strings.TrimSpace(c.UUID))
				cfdiByUUID[u] = append(cfdiByUUID[u], c)
			}
		}
	}

	payByID := make(map[string]tenant.Payment)
	if len(payIDs) > 0 {
		idList := make([]string, 0, len(payIDs))
		for id := range payIDs {
			idList = append(idList, id)
		}
		var pays []tenant.Payment
		if err := conn.NewSelect().Model(&pays).
			Where("company_identifier = ?", companyID).
			Where("identifier IN (?)", bun.In(idList)).
			Scan(ctx); err == nil {
			for _, p := range pays {
				payByID[p.Identifier] = p
			}
		}
	}

	formaNames := make(map[string]string)
	codes := make(map[string]struct{})
	for _, p := range payByID {
		c := strings.TrimSpace(p.FormaDePagoP)
		if c != "" {
			codes[c] = struct{}{}
		}
	}
	if len(codes) > 0 {
		codeList := make([]string, 0, len(codes))
		for c := range codes {
			codeList = append(codeList, c)
		}
		type row struct {
			Code string `bun:"code"`
			Name string `bun:"name"`
		}
		var fr []row
		_ = conn.NewSelect().
			ColumnExpr("code, name").
			TableExpr("cat_forma_pago").
			Where("code IN (?)", bun.In(codeList)).
			Scan(ctx, &fr)
		for _, r := range fr {
			formaNames[r.Code] = r.Name
		}
	}

	for i := range result.Data {
		rec := result.Data[i]
		iss, _ := rec["is_issued"].(bool)
		origin, _ := rec["UUID"].(string)
		rel, _ := rec["UUID_related"].(string)
		pid, _ := rec["payment_identifier"].(string)

		originCFDI := pickCFDIByUUID(cfdiByUUID, origin, iss)
		relCFDI := pickCFDIByUUID(cfdiByUUID, rel, iss)

		rec["cfdi_origin"] = cfdiRelatedSlim(originCFDI)
		rec["cfdi_related"] = cfdiRelatedSlim(relCFDI)

		if p, ok := payByID[strings.TrimSpace(pid)]; ok {
			pm := crud.SerializeOne(p)
			pm["c_forma_pago"] = map[string]interface{}{
				"name": formaNames[strings.TrimSpace(p.FormaDePagoP)],
			}
			rec["payment_related"] = pm
		} else {
			rec["payment_related"] = nil
		}
		result.Data[i] = rec
	}
}
