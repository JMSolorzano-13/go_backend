package cfdi

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

type ISRGetter struct {
	DB  bun.IDB
	Ctx context.Context
}

func (g *ISRGetter) GetISR(period time.Time, companyData map[string]interface{}) map[string]interface{} {
	deductions := CalcDeduccionesAutorizadasYCompras(g.Ctx, g.DB, period, companyData)

	periodRes := g.getISRTimeWindow(period, false, deductions)
	exerciseRes := g.getISRTimeWindow(period, true, deductions)
	return map[string]interface{}{
		"period":   periodRes,
		"exercise": exerciseRes,
	}
}

func (g *ISRGetter) getISRTimeWindow(period time.Time, yearly bool, deductions map[string]interface{}) map[string]interface{} {
	incomes := g.getISRForIssued(period, true, yearly)
	return map[string]interface{}{
		"incomes":    incomes,
		"deductions": deductions,
	}
}

func (g *ISRGetter) getISRForIssued(period time.Time, issued bool, yearly bool) map[string]interface{} {
	dates := GetWindowDates(period, yearly)
	filters := g.buildISRFilters(dates, issued)

	for key := range filters {
		filters[key] = addISRCommonFilter(filters[key], issued, "all")
	}

	commonFields := []string{`"BaseIVA16"`, `"BaseIVA8"`, `"BaseIVA0"`, `"BaseIVAExento"`}
	retenciones := `"RetencionesISRMXN"`

	components := make(map[string]interface{})

	// invoice_pue
	invResult := g.queryISRComponent(commonFields, []string{retenciones}, filters["invoice_pue"])
	components["invoice_pue"] = invResult

	// payments
	payResult := g.queryISRComponent(commonFields, []string{retenciones}, filters["payments"])
	components["payments"] = payResult

	totalSum := toFloat(invResult["total"]) + toFloat(payResult["total"])
	totalQty := toInt(invResult["qty"]) + toInt(payResult["qty"])
	components["total"] = round2(totalSum)
	components["qty"] = totalQty
	components["excluded_qty"] = g.getISRExcludedQty(issued, period, yearly)

	return components
}

func (g *ISRGetter) queryISRComponent(totalFields []string, infoFields []string, filter string) map[string]interface{} {
	allFields := append(append([]string{}, totalFields...), infoFields...)
	sums := ""
	for i, f := range allFields {
		if i > 0 {
			sums += ", "
		}
		sums += fmt.Sprintf("COALESCE(SUM(%s), 0) AS %s", f, f)
	}
	q := fmt.Sprintf("SELECT %s, COUNT(*) AS qty FROM cfdi WHERE %s", sums, filter)

	row := g.DB.QueryRowContext(g.Ctx, q)
	vals := make([]interface{}, len(allFields)+1)
	ptrs := make([]interface{}, len(allFields)+1)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	res := make(map[string]interface{}, len(allFields)+2)
	if err := row.Scan(ptrs...); err != nil {
		for _, f := range allFields {
			res[trimQuotes(f)] = 0.0
		}
		res["qty"] = 0
		res["total"] = 0.0
		return res
	}
	for i, f := range allFields {
		res[trimQuotes(f)] = round2(scanFloat(vals[i]))
	}
	res["qty"] = scanInt(vals[len(allFields)])

	total := 0.0
	for _, f := range totalFields {
		total += toFloat(res[trimQuotes(f)])
	}
	res["total"] = round2(total)
	return res
}

func (g *ISRGetter) getISRExcludedQty(issued bool, period time.Time, yearly bool) int {
	issuedStr := "true"
	if !issued {
		issuedStr = "false"
	}
	orFilter := g.GetOrFilters(period, yearly, issued, `"FechaFiltro"`)
	basic := fmt.Sprintf(`"is_issued" IS %s AND "Estatus" AND "Version" = '4.0'`, issuedStr)
	q := fmt.Sprintf(`SELECT COUNT(*) FROM cfdi WHERE %s AND %s AND "ExcludeFromISR"`, orFilter, basic)
	var count int
	g.DB.QueryRowContext(g.Ctx, q).Scan(&count)
	return count
}

func (g *ISRGetter) buildISRFilters(dates WindowDates, issued bool) map[string]string {
	dateField := `"FechaFiltro"`
	window := fmt.Sprintf(`%s BETWEEN '%s' AND '%s'`, dateField, fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
	isPago := `"TipoDeComprobante" = 'P'`
	isIngreso := `("TipoDeComprobante" = 'I' AND "MetodoPago" = 'PUE')`

	return map[string]string{
		"invoice_pue": "(" + isIngreso + " AND " + window + ")",
		"payments":    "(" + isPago + " AND " + window + ")",
	}
}

func (g *ISRGetter) GetOrFilters(period time.Time, yearly bool, issued bool, dateField string) string {
	dates := GetWindowDates(period, yearly)
	if dateField == "" {
		dateField = `"FechaFiltro"`
	}
	window := fmt.Sprintf(`%s BETWEEN '%s' AND '%s'`, dateField, fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
	isPago := `"TipoDeComprobante" = 'P'`
	isIngreso := `("TipoDeComprobante" = 'I' AND "MetodoPago" = 'PUE')`
	return fmt.Sprintf("(%s AND %s) OR (%s AND %s)", isIngreso, window, isPago, window)
}

func (g *ISRGetter) GetFullFilter(period time.Time, yearly bool, isr string, issued bool) string {
	dates := GetWindowDates(period, yearly)
	if isr == "all" {
		orFilter := g.GetOrFilters(period, yearly, issued, "")
		return addISRCommonFilter(orFilter, issued, isr)
	}
	if isr == "moved" || isr == "excluded" {
		orFilter := g.GetOrFilters(period, yearly, issued, `"FechaFiltro"`)
		return addISRCommonFilter(orFilter, issued, isr)
	}
	filters := g.buildISRFilters(dates, issued)
	if f, ok := filters[isr]; ok {
		return addISRCommonFilter(f, issued, isr)
	}
	return "TRUE"
}

func (g *ISRGetter) GetExportDisplayName(isr string, issued bool) string {
	isrNames := map[string]string{
		"all": "", "invoice_pue": "Facturas de contado", "payments": "CFDI de pagos",
		"moved": "Periodo ISR Reasignado", "excluded": "No considerados ISR",
	}
	issuedName := "Deducciones"
	if issued {
		issuedName = "Ingresos"
	}
	if n := isrNames[isr]; n != "" {
		return issuedName + " - " + n
	}
	return issuedName
}

func addISRCommonFilter(filter string, issued bool, isr string) string {
	issuedStr := "true"
	if !issued {
		issuedStr = "false"
	}
	excludeClause := `NOT "ExcludeFromISR"`
	if isr == "excluded" {
		excludeClause = `"ExcludeFromISR"`
	}
	return fmt.Sprintf(`(%s AND "is_issued" IS %s AND "Estatus" AND "Version" = '4.0' AND %s)`, filter, issuedStr, excludeClause)
}
