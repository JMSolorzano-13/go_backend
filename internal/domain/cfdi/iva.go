package cfdi

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uptrace/bun"
)

type WindowDates struct {
	PeriodOrExerciseStart time.Time
	PeriodEnd             time.Time
	PrevStart             time.Time
	PrevEnd               time.Time
	PeriodStart           time.Time
}

func GetWindowDates(period time.Time, yearly bool) WindowDates {
	periodStart := time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC)
	exerciseStart := periodStart
	if yearly {
		exerciseStart = time.Date(period.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	}
	periodEnd := endMonth(periodStart)
	prevMonth := exerciseStart.AddDate(0, -1, 0)
	prevStart := time.Date(prevMonth.Year(), prevMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
	prevEnd := endMonth(prevStart)

	return WindowDates{
		PeriodOrExerciseStart: exerciseStart,
		PeriodEnd:             periodEnd,
		PrevStart:             prevStart,
		PrevEnd:               prevEnd,
		PeriodStart:           periodStart,
	}
}

type IVAGetter struct {
	DB  bun.IDB
	Ctx context.Context
}

func (g *IVAGetter) GetIVA(period time.Time) map[string]interface{} {
	periodRes := g.getTimeWindow(period, false)
	exerciseRes := g.getTimeWindow(period, true)
	return map[string]interface{}{
		"period":   periodRes,
		"exercise": exerciseRes,
	}
}

func (g *IVAGetter) GetIVAAll(period time.Time) map[string]interface{} {
	year := period.Year()
	endMo := int(period.Month())

	exercise := g.getTimeWindow(period, true)

	var mu sync.Mutex
	results := make(map[string]interface{}, endMo)
	var wg sync.WaitGroup
	for m := 1; m <= endMo; m++ {
		wg.Add(1)
		go func(month int) {
			defer wg.Done()
			monthlyPeriod := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			periodData := g.getTimeWindow(monthlyPeriod, false)
			key := fmt.Sprintf("%02d", month)
			mu.Lock()
			results[key] = map[string]interface{}{
				"period":   periodData,
				"exercise": exercise,
			}
			mu.Unlock()
		}(m)
	}
	wg.Wait()
	return results
}

func (g *IVAGetter) getTimeWindow(period time.Time, yearly bool) map[string]interface{} {
	creditable := g.getIVAForIssued(period, false, yearly)
	transferred := g.getIVAForIssued(period, true, yearly)
	credTotal := toFloat(creditable["total"])
	transTotal := toFloat(transferred["total"])
	return map[string]interface{}{
		"creditable":  creditable,
		"transferred": transferred,
		"diff":        round2(transTotal - credTotal),
	}
}

func (g *IVAGetter) getIVAForIssued(period time.Time, issued bool, yearly bool) map[string]interface{} {
	dates := GetWindowDates(period, yearly)
	ivaFilters := buildIVACFDIFilters(dates, issued)

	commonFields := []string{
		`"BaseIVA16"`, `"BaseIVA8"`, `"BaseIVA0"`, `"BaseIVAExento"`,
		`"IVATrasladado16"`, `"IVATrasladado8"`, `"pr_count"`,
	}
	doctoFields := []string{
		`"BaseIVA16"`, `"BaseIVA8"`, `"BaseIVA0"`, `"BaseIVAExento"`,
		`"IVATrasladado16"`, `"IVATrasladado8"`, `"RetencionesIVAMXN"`,
	}

	// Apply common filters to each non-special key
	for key := range ivaFilters {
		if key == "excluded" || key == "moved" {
			continue
		}
		if !issued && (key == "p_tra" || key == "curr_p_ret") {
			continue
		}
		ivaFilters[key] = addIVACommonFilter(ivaFilters[key], issued, "all")
	}

	components := make(map[string]interface{})

	// i_tra
	fields := append([]string{`"TrasladosIVAMXN"`}, commonFields...)
	components["i_tra"] = g.querySumCount("cfdi", fields, ivaFilters["i_tra"], false)

	// p_tra
	if issued {
		fields = append([]string{`"TrasladosIVAMXN"`}, commonFields...)
		components["p_tra"] = g.querySumCount("cfdi", fields, ivaFilters["p_tra"], false)
	} else {
		components["p_tra"] = g.queryDoctoSumCount(doctoFields, ivaFilters["p_tra"])
	}

	// credit_notes
	fields = append([]string{`"RetencionesIVAMXN"`}, commonFields...)
	fields = append(fields, `"TrasladosIVAMXN"`)
	cn := g.querySumCount("cfdi", fields, ivaFilters["credit_notes"], false)
	cn["total"] = round2(toFloat(cn["IVATrasladado16"]) + toFloat(cn["IVATrasladado8"]))
	components["credit_notes"] = cn

	// curr_i_ret
	currIRet := g.querySumCount("cfdi", []string{`"RetencionesIVAMXN"`}, ivaFilters["curr_i_ret"], false)
	// curr_p_ret
	var currPRet map[string]interface{}
	if issued {
		currPRet = g.querySumCount("cfdi", []string{`"RetencionesIVAMXN"`}, ivaFilters["curr_p_ret"], false)
	} else {
		currPRet = g.queryDoctoSumCount(doctoFields, ivaFilters["curr_p_ret"])
	}

	iTra := components["i_tra"].(map[string]interface{})
	iTra["RetencionesIVAMXN"] = currIRet["total"]
	pTra := components["p_tra"].(map[string]interface{})
	pTra["RetencionesIVAMXN"] = toFloat(currPRet["RetencionesIVAMXN"])

	totalSum := 0.0
	totalQty := 0
	for k, c := range components {
		cm := c.(map[string]interface{})
		if k != "credit_notes" {
			totalSum += toFloat(cm["total"])
		}
		totalQty += toInt(cm["qty"])
	}
	components["total"] = round2(totalSum)
	components["qty"] = totalQty
	components["excluded_qty"] = g.getExcludedQty(issued, period, yearly)
	components["moved_qty"] = g.getMovedQty(issued, period, yearly)

	return components
}

func (g *IVAGetter) querySumCount(table string, fields []string, filter string, firstIsTotal bool) map[string]interface{} {
	sums := ""
	for i, f := range fields {
		if i > 0 {
			sums += ", "
		}
		sums += fmt.Sprintf("COALESCE(SUM(%s), 0) AS %s", f, f)
	}
	q := fmt.Sprintf("SELECT %s, COUNT(*) AS qty FROM %s WHERE %s", sums, table, filter)
	row := g.DB.QueryRowContext(g.Ctx, q)

	vals := make([]interface{}, len(fields)+1)
	ptrs := make([]interface{}, len(fields)+1)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	res := make(map[string]interface{}, len(fields)+2)
	if err := row.Scan(ptrs...); err != nil {
		for _, f := range fields {
			res[trimQuotes(f)] = 0.0
		}
		res["qty"] = 0
		res["total"] = 0.0
		return res
	}

	for i, f := range fields {
		res[trimQuotes(f)] = round2(scanFloat(vals[i]))
	}
	res["qty"] = scanInt(vals[len(fields)])
	res["total"] = res[trimQuotes(fields[0])]
	return res
}

func (g *IVAGetter) queryDoctoSumCount(fields []string, filter string) map[string]interface{} {
	sums := ""
	for i, f := range fields {
		if i > 0 {
			sums += ", "
		}
		sums += fmt.Sprintf("COALESCE(SUM(pr.%s), 0) AS %s", f, f)
	}
	q := fmt.Sprintf(`SELECT %s, COUNT(1) AS qty
		FROM payment_relation pr
		JOIN cfdi c ON pr."UUID_related" = c."UUID"
		WHERE NOT pr."ExcludeFromIVA"
		  AND NOT pr."is_issued"
		  AND pr."Estatus"
		  AND c."TipoDeComprobante" = 'I'
		  AND (c.from_xml OR c.is_too_big)
		  AND NOT c."is_issued"
		  AND c."Estatus"
		  AND pr.%s`, sums, filter)

	row := g.DB.QueryRowContext(g.Ctx, q)
	vals := make([]interface{}, len(fields)+1)
	ptrs := make([]interface{}, len(fields)+1)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	res := make(map[string]interface{}, len(fields)+2)
	if err := row.Scan(ptrs...); err != nil {
		for _, f := range fields {
			res[trimQuotes(f)] = 0.0
		}
		res["qty"] = 0
		res["total"] = 0.0
		return res
	}
	for i, f := range fields {
		res[trimQuotes(f)] = round2(scanFloat(vals[i]))
	}
	res["qty"] = scanInt(vals[len(fields)])
	res["total"] = round2(toFloat(res["IVATrasladado16"]) + toFloat(res["IVATrasladado8"]))
	return res
}

func (g *IVAGetter) getExcludedQty(issued bool, period time.Time, yearly bool) int {
	dates := GetWindowDates(period, yearly)
	issuedStr := "true"
	if !issued {
		issuedStr = "false"
	}

	if issued {
		dateFilter := fmt.Sprintf(`"FechaFiltro" BETWEEN '%s' AND '%s'`,
			fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
		q := fmt.Sprintf(`SELECT COUNT(*) FROM cfdi WHERE "is_issued" IS %s AND "Estatus" AND "Version" = '4.0' AND "ExcludeFromIVA" AND %s`,
			issuedStr, dateFilter)
		var count int
		g.DB.QueryRowContext(g.Ctx, q).Scan(&count)
		return count
	}

	dateFilter := fmt.Sprintf(`"FechaFiltro" BETWEEN '%s' AND '%s'`,
		fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
	cfdisQ := fmt.Sprintf(`SELECT COUNT(*) FROM cfdi WHERE "is_issued" IS %s AND "Estatus" AND "Version" = '4.0' AND "ExcludeFromIVA" AND "TipoDeComprobante" IN ('I','E') AND %s`,
		issuedStr, dateFilter)
	var cfdisExcl int
	g.DB.QueryRowContext(g.Ctx, cfdisQ).Scan(&cfdisExcl)

	doctoQ := fmt.Sprintf(`SELECT COUNT(*) FROM payment_relation
		WHERE is_issued IS false AND "ExcludeFromIVA" AND "Estatus"
		AND "FechaPago" BETWEEN '%s' AND '%s'`,
		fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
	var doctoExcl int
	g.DB.QueryRowContext(g.Ctx, doctoQ).Scan(&doctoExcl)

	return cfdisExcl + doctoExcl
}

func (g *IVAGetter) getMovedQty(issued bool, period time.Time, yearly bool) int {
	dates := GetWindowDates(period, yearly)
	issuedStr := "true"
	if !issued {
		issuedStr = "false"
	}
	dateFilter := fmt.Sprintf(`"FechaFiltro" BETWEEN '%s' AND '%s'`,
		fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
	q := fmt.Sprintf(`SELECT COUNT(*) FROM cfdi WHERE "is_issued" IS %s AND "Estatus" AND "Version" = '4.0' AND NOT "ExcludeFromIVA" AND %s AND "FechaFiltro" != "PaymentDate"`,
		issuedStr, dateFilter)
	var count int
	g.DB.QueryRowContext(g.Ctx, q).Scan(&count)
	return count
}

// -- Export helpers --

func (g *IVAGetter) GetFullFilter(period time.Time, yearly bool, iva string, issued bool) string {
	dates := GetWindowDates(period, yearly)
	if iva == "all" {
		return g.GetOrFilters(period, yearly, issued, "")
	}
	if iva == "moved" || iva == "excluded" {
		fechaDates := GetWindowDates(period, yearly)
		dateField := `"FechaFiltro"`
		window := fmt.Sprintf(`%s BETWEEN '%s' AND '%s'`, dateField, fmtDate(fechaDates.PeriodOrExerciseStart), fmtDate(fechaDates.PeriodEnd))
		isPago := `"TipoDeComprobante" = 'P'`
		isIngreso := `("TipoDeComprobante" = 'I' AND "MetodoPago" = 'PUE')`
		isEgreso := `"TipoDeComprobante" = 'E'`

		var ivaFilter string
		if issued {
			ivaFilter = fmt.Sprintf("(%s OR %s OR %s) AND %s", isIngreso, isEgreso, isPago, window)
		} else {
			ivaFilter = fmt.Sprintf("(%s OR %s) AND %s", isIngreso, isEgreso, window)
		}
		return addIVACommonFilter(ivaFilter, issued, iva)
	}
	filters := buildIVACFDIFilters(dates, issued)
	if f, ok := filters[iva]; ok {
		return addIVACommonFilter(f, issued, iva)
	}
	return "TRUE"
}

func (g *IVAGetter) GetExportDisplayName(period time.Time, yearly bool, iva string, issued bool) string {
	ivaNames := map[string]string{
		"all": "", "i_tra": "Facturas de contado", "p_tra": "Facturas de crédito",
		"totals": "Totales", "moved": "Periodo IVA Reasignado",
		"excluded": "No considerados IVA", "credit_notes": "Notas de crédito",
		"OpeConTer": "Operaciones con terceros",
	}
	issuedName := "Acreditable"
	if issued {
		issuedName = "Trasladado"
	}
	cobroPago := ""
	if name := ivaNames[iva]; strContains(name, "crédito") {
		if issued {
			cobroPago = "Cobro "
		} else {
			cobroPago = "Pago "
		}
	}
	name := issuedName
	if n := ivaNames[iva]; n != "" {
		name += " - " + cobroPago + n
	}
	return name
}

func (g *IVAGetter) GetOrFilters(period time.Time, yearly bool, issued bool, dateField string) string {
	dates := GetWindowDates(period, yearly)
	if dateField == "" {
		dateField = `"PaymentDate"`
	}
	window := fmt.Sprintf(`%s BETWEEN '%s' AND '%s'`, dateField, fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
	periodWindow := fmt.Sprintf(`%s BETWEEN '%s' AND '%s'`, dateField, fmtDate(dates.PeriodStart), fmtDate(dates.PeriodEnd))

	isPago := `"TipoDeComprobante" = 'P'`
	isIngreso := `("TipoDeComprobante" = 'I' AND "MetodoPago" = 'PUE')`
	isEgreso := `"TipoDeComprobante" = 'E'`

	parts := []string{
		"(" + isIngreso + " AND " + window + ")",
		"(" + isPago + " AND " + window + ")",
		"(" + isEgreso + " AND " + window + ")",
		"(" + isIngreso + " AND " + periodWindow + ")",
		"(" + isPago + " AND " + periodWindow + ")",
	}
	return "(" + joinOR(parts) + ")"
}

// buildIVACFDIFilters creates the filter SQL strings for IVA components.
func buildIVACFDIFilters(dates WindowDates, issued bool) map[string]string {
	dateField := `"PaymentDate"`
	window := fmt.Sprintf(`%s BETWEEN '%s' AND '%s'`, dateField, fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
	periodWindow := fmt.Sprintf(`%s BETWEEN '%s' AND '%s'`, dateField, fmtDate(dates.PeriodStart), fmtDate(dates.PeriodEnd))

	doctoWindow := fmt.Sprintf(`"FechaPago" BETWEEN '%s' AND '%s'`, fmtDate(dates.PeriodOrExerciseStart), fmtDate(dates.PeriodEnd))
	doctoCurrentWindow := fmt.Sprintf(`"FechaPago" BETWEEN '%s' AND '%s'`, fmtDate(dates.PeriodStart), fmtDate(dates.PeriodEnd))

	isPago := `"TipoDeComprobante" = 'P'`
	isIngreso := `("TipoDeComprobante" = 'I' AND "MetodoPago" = 'PUE')`
	isEgreso := `"TipoDeComprobante" = 'E'`

	filters := map[string]string{
		"i_tra":        "(" + isIngreso + " AND " + window + ")",
		"credit_notes": "(" + isEgreso + " AND " + window + ")",
		"curr_i_ret":   "(" + isIngreso + " AND " + periodWindow + ")",
	}

	if issued {
		filters["p_tra"] = "(" + isPago + " AND " + window + ")"
		filters["curr_p_ret"] = "(" + isPago + " AND " + periodWindow + ")"
	} else {
		filters["p_tra"] = doctoWindow
		filters["curr_p_ret"] = doctoCurrentWindow
	}

	filters["excluded"] = "(" + filters["i_tra"] + " OR " + filters["credit_notes"] + " OR " + filters["p_tra"] + ")"
	if issued {
		filters["moved"] = filters["excluded"]
	} else {
		filters["moved"] = "(" + filters["i_tra"] + " OR " + filters["credit_notes"] + ")"
	}

	return filters
}

func addIVACommonFilter(filter string, issued bool, iva string) string {
	issuedStr := "true"
	if !issued {
		issuedStr = "false"
	}
	excludeClause := `NOT "ExcludeFromIVA"`
	if iva == "excluded" {
		excludeClause = `"ExcludeFromIVA"`
	}
	return fmt.Sprintf(`(%s AND "is_issued" IS %s AND "Estatus" AND "Version" = '4.0' AND %s)`, filter, issuedStr, excludeClause)
}
