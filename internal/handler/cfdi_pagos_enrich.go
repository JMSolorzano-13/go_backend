package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/uptrace/bun"

	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
)

func cfdiSearchNeedsPaymentNested(fields []string, rows []map[string]interface{}) bool {
	for _, f := range fields {
		if strings.HasPrefix(f, "payments.") || strings.HasPrefix(f, "pays.") {
			return true
		}
	}
	for _, row := range rows {
		if t, ok := row["TipoDeComprobante"].(string); ok && t == "P" {
			return true
		}
	}
	return false
}

func paymentGroupKey(uuid string, isIssued bool) string {
	return fmt.Sprintf("%s|%t", strings.ToLower(strings.TrimSpace(uuid)), isIssued)
}

func cfdiRelatedSlim(c *tenant.CFDI) map[string]interface{} {
	if c == nil {
		return nil
	}
	m := map[string]interface{}{
		"UUID":              c.UUID,
		"MetodoPago":        c.MetodoPago,
		"UsoCFDIReceptor":   c.UsoCFDIReceptor,
		"Estatus":           c.Estatus,
		"TipoDeComprobante": c.TipoDeComprobante,
	}
	if c.Folio != nil {
		m["Folio"] = *c.Folio
	} else {
		m["Folio"] = nil
	}
	if c.Serie != nil {
		m["Serie"] = *c.Serie
	} else {
		m["Serie"] = nil
	}
	if c.Fecha.IsZero() {
		m["Fecha"] = nil
	} else {
		m["Fecha"] = c.Fecha.Format(crud.APITimestampFormat)
	}
	return m
}

func pickCFDIByUUID(candidates map[string][]tenant.CFDI, uuid string, preferIssued bool) *tenant.CFDI {
	u := strings.ToLower(strings.TrimSpace(uuid))
	list := candidates[u]
	for _, tryIssued := range []bool{preferIssued, !preferIssued} {
		for i := range list {
			if list[i].IsIssued == tryIssued {
				return &list[i]
			}
		}
	}
	if len(list) > 0 {
		return &list[0]
	}
	return nil
}

// enrichCFDIsWithPagosData attaches payments, pays, and nested cfdi_related for tipo P rows
// (Python SQLAlchemy CFDI.payments / CFDI.pays).
func enrichCFDIsWithPagosData(ctx context.Context, conn bun.Conn, companyID string, result *crud.SearchResult, params crud.SearchParams) {
	if result == nil || len(result.Data) == 0 {
		return
	}
	if !cfdiSearchNeedsPaymentNested(params.Fields, result.Data) {
		return
	}

	pagoUUIDs := make([]string, 0)
	for _, rec := range result.Data {
		t, _ := rec["TipoDeComprobante"].(string)
		if t != "P" {
			continue
		}
		u, ok := rec["UUID"].(string)
		if !ok || strings.TrimSpace(u) == "" {
			continue
		}
		pagoUUIDs = append(pagoUUIDs, strings.ToLower(strings.TrimSpace(u)))
	}
	if len(pagoUUIDs) == 0 {
		return
	}

	var payments []tenant.Payment
	if err := conn.NewSelect().Model(&payments).
		Where("company_identifier = ?", companyID).
		Where("uuid_origin IN (?)", bun.In(pagoUUIDs)).
		Scan(ctx); err != nil {
		return
	}

	var doctos []tenant.DoctoRelacionado
	if err := conn.NewSelect().Model(&doctos).
		Where("company_identifier = ?", companyID).
		Where(`"UUID" IN (?)`, bun.In(pagoUUIDs)).
		Where(`"Estatus" = ?`, true).
		Scan(ctx); err != nil {
		return
	}

	relatedSet := make(map[string]struct{})
	for _, d := range doctos {
		ur := strings.ToLower(strings.TrimSpace(d.UUIDRelated))
		if ur != "" {
			relatedSet[ur] = struct{}{}
		}
	}
	relatedList := make([]string, 0, len(relatedSet))
	for u := range relatedSet {
		relatedList = append(relatedList, u)
	}

	cfdiByUUID := make(map[string][]tenant.CFDI)
	if len(relatedList) > 0 {
		var relCfdis []tenant.CFDI
		if err := conn.NewSelect().Model(&relCfdis).
			Where("company_identifier = ?", companyID).
			Where(`"UUID" IN (?)`, bun.In(relatedList)).
			Scan(ctx); err == nil {
			for _, c := range relCfdis {
				u := strings.ToLower(strings.TrimSpace(c.UUID))
				cfdiByUUID[u] = append(cfdiByUUID[u], c)
			}
		}
	}

	formaNames := make(map[string]string)
	codes := make(map[string]struct{})
	for _, p := range payments {
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

	payByOrigin := make(map[string][]tenant.Payment)
	for _, p := range payments {
		k := paymentGroupKey(p.UUIDOrigin, p.IsIssued)
		payByOrigin[k] = append(payByOrigin[k], p)
	}

	drByOrigin := make(map[string][]tenant.DoctoRelacionado)
	for _, d := range doctos {
		k := paymentGroupKey(d.UUID, d.IsIssued)
		drByOrigin[k] = append(drByOrigin[k], d)
	}

	for i, rec := range result.Data {
		t, _ := rec["TipoDeComprobante"].(string)
		if t != "P" {
			continue
		}
		u, ok := rec["UUID"].(string)
		if !ok {
			continue
		}
		issued, _ := rec["is_issued"].(bool)
		k := paymentGroupKey(u, issued)

		paysOut := make([]interface{}, 0)
		for _, dr := range drByOrigin[k] {
			dm := crud.SerializeOne(dr)
			rel := pickCFDIByUUID(cfdiByUUID, dr.UUIDRelated, dr.IsIssued)
			dm["cfdi_related"] = cfdiRelatedSlim(rel)
			paysOut = append(paysOut, dm)
		}

		paymentsOut := make([]interface{}, 0)
		for _, p := range payByOrigin[k] {
			pm := crud.SerializeOne(p)
			name := formaNames[strings.TrimSpace(p.FormaDePagoP)]
			pm["c_forma_pago"] = map[string]interface{}{"name": name}
			paymentsOut = append(paymentsOut, pm)
		}

		result.Data[i]["payments"] = paymentsOut
		result.Data[i]["pays"] = paysOut
	}
}
