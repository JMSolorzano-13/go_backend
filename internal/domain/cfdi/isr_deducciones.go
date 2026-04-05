package cfdi

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

const defaultISRPct = 0.47

func getISRPercentage(companyData map[string]interface{}) float64 {
	if companyData == nil {
		return defaultISRPct
	}
	if v, ok := companyData["isr_percentage"]; ok {
		return toFloat(v)
	}
	return defaultISRPct
}

func fechaFiltroRange(period time.Time) (string, string) {
	start := time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(period.Year(), period.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return fmtTimestamp(start), fmtTimestamp(end)
}

func queryGastosNominaGravada(ctx context.Context, db bun.IDB, period time.Time) (int, float64) {
	start, end := fechaFiltroRange(period)
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(n."PercepcionesTotalGravado"), 0)::numeric
		FROM cfdi c
		LEFT JOIN nomina n ON c."UUID" = n.cfdi_uuid
		WHERE c."TipoDeComprobante" = 'N' AND c.is_issued AND c."Estatus"
		  AND c."Version" = '4.0'
		  AND c."FechaFiltro" >= '%s' AND c."FechaFiltro" < '%s'`, start, end)
	var count int
	var total float64
	db.QueryRowContext(ctx, q).Scan(&count, &total)
	return count, total
}

func queryGastosNominaExentoTotal(ctx context.Context, db bun.IDB, period time.Time) float64 {
	start, end := fechaFiltroRange(period)
	q := fmt.Sprintf(`SELECT COALESCE(SUM(n."PercepcionesTotalExento"), 0)::numeric
		FROM cfdi c
		JOIN nomina n ON c."UUID" = n.cfdi_uuid
		WHERE c."TipoDeComprobante" = 'N' AND c.is_issued AND c."Estatus"
		  AND c."Version" = '4.0'
		  AND c."FechaFiltro" >= '%s' AND c."FechaFiltro" < '%s'`, start, end)
	var total float64
	db.QueryRowContext(ctx, q).Scan(&total)
	return total
}

func comprasGastosFacturasContado(ctx context.Context, db bun.IDB, period time.Time, excludeFromISR bool) (int, float64, float64) {
	start, end := fechaFiltroRange(period)
	bancarizadas := "'" + strings.Join(FormaPagoBancarizadasList(), "','") + "'"
	usoCFDI := "'" + strings.Join(UsoCFDIBancarizadasList(), "','") + "'"
	exclStr := "false"
	if excludeFromISR {
		exclStr = "true"
	}
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM("NetoMXN"), 0)::numeric, COALESCE(SUM("RetencionesISRMXN"), 0)::numeric
		FROM cfdi
		WHERE "TipoDeComprobante" = 'I' AND NOT is_issued AND "Estatus" AND "Version" = '4.0'
		  AND "MetodoPago" = 'PUE'
		  AND "FormaPago" IN (%s) AND "UsoCFDIReceptor" IN (%s)
		  AND "ExcludeFromISR" IS %s
		  AND "FechaFiltro" >= '%s' AND "FechaFiltro" <= '%s'`, bancarizadas, usoCFDI, exclStr, start, end)
	var count int
	var neto, isr float64
	db.QueryRowContext(ctx, q).Scan(&count, &neto, &isr)
	return count, neto, isr
}

func comprasGastosCFDIPagos(ctx context.Context, db bun.IDB, period time.Time, excludeFromISR bool) (int, float64, float64) {
	start, end := fechaFiltroRange(period)
	bancarizadas := "'" + strings.Join(FormaPagoBancarizadasList(), "','") + "'"
	usoCFDI := "'" + strings.Join(UsoCFDIBancarizadasList(), "','") + "'"
	exclStr := "false"
	if excludeFromISR {
		exclStr = "true"
	}
	q := fmt.Sprintf(`SELECT COUNT(cfdi_pago."UUID"), COALESCE(SUM(pr."ImpPagadoMXN"), 0)::numeric,
		COALESCE(SUM(
			(SELECT COALESCE(SUM((value->>'@ImporteDR')::numeric), 0)
			 FROM jsonb_array_elements(pr."RetencionesDR") AS value
			 WHERE value->>'@ImpuestoDR' = '001')
		), 0)::numeric
		FROM payment_relation pr
		JOIN cfdi cfdi_pago ON pr."UUID" = cfdi_pago."UUID"
		JOIN payment p ON pr.payment_identifier = p.identifier
		JOIN cfdi i ON pr."UUID_related" = i."UUID"
		WHERE cfdi_pago."TipoDeComprobante" = 'P'
		  AND p."FormaDePagoP" IN (%s) AND i."UsoCFDIReceptor" IN (%s)
		  AND cfdi_pago."ExcludeFromISR" IS %s
		  AND cfdi_pago."Estatus" IS true AND NOT cfdi_pago.is_issued
		  AND pr."FechaPago" >= '%s' AND pr."FechaPago" < '%s'`,
		bancarizadas, usoCFDI, exclStr, start, end)
	var count int
	var neto, isr float64
	db.QueryRowContext(ctx, q).Scan(&count, &neto, &isr)
	return count, neto, isr
}

func devDesctosBonifIngresosEmitidos(ctx context.Context, db bun.IDB, period time.Time, excludeFromISR bool) (int, float64) {
	start, end := fechaFiltroRange(period)
	exclStr := "false"
	if excludeFromISR {
		exclStr = "true"
	}
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM("DescuentoMXN"), 0)::numeric
		FROM cfdi
		WHERE "TipoDeComprobante" = 'I' AND is_issued AND "Estatus" AND "MetodoPago" = 'PUE'
		  AND "ExcludeFromISR" IS %s AND "Version" = '4.0'
		  AND "FechaFiltro" >= '%s' AND "FechaFiltro" < '%s'`, exclStr, start, end)
	var count int
	var total float64
	db.QueryRowContext(ctx, q).Scan(&count, &total)
	return count, total
}

func devDesctosBonifEgresosEmitidos(ctx context.Context, db bun.IDB, period time.Time, excludeFromISR bool) (int, float64) {
	start, end := fechaFiltroRange(period)
	exclStr := "false"
	if excludeFromISR {
		exclStr = "true"
	}
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM("NetoMXN"), 0)::numeric
		FROM cfdi
		WHERE "TipoDeComprobante" = 'E' AND is_issued AND "Estatus" AND "MetodoPago" = 'PUE'
		  AND "ExcludeFromISR" IS %s AND "Version" = '4.0'
		  AND "FechaFiltro" >= '%s' AND "FechaFiltro" < '%s'`, exclStr, start, end)
	var count int
	var total float64
	db.QueryRowContext(ctx, q).Scan(&count, &total)
	return count, total
}

func devPagosProvisionales(ctx context.Context, db bun.IDB, period time.Time) (int, float64) {
	cIng, iIng := devDesctosBonifIngresosEmitidos(ctx, db, period, false)
	cEgr, iEgr := devDesctosBonifEgresosEmitidos(ctx, db, period, false)
	return cIng + cEgr, iIng + iEgr
}

func noConsideradosIngresosPUE(ctx context.Context, db bun.IDB, period time.Time, excludeFromISR bool) (int, float64, float64) {
	start, end := fechaFiltroRange(period)
	noBancarizadas := "'" + strings.Join(FormaPagoNoBancarizadasList(), "','") + "'"
	exclStr := "false"
	if excludeFromISR {
		exclStr = "true"
	}
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM("NetoMXN"), 0)::numeric, COALESCE(SUM("RetencionesISRMXN"), 0)::numeric
		FROM cfdi
		WHERE "TipoDeComprobante" = 'I' AND "MetodoPago" = 'PUE' AND NOT is_issued
		  AND "Estatus" AND "Version" = '4.0'
		  AND "FormaPago" IN (%s)
		  AND "ExcludeFromISR" IS %s
		  AND "FechaFiltro" >= '%s' AND "FechaFiltro" < '%s'`, noBancarizadas, exclStr, start, end)
	var count int
	var neto, isr float64
	db.QueryRowContext(ctx, q).Scan(&count, &neto, &isr)
	return count, neto, isr
}

func noConsideradosPagos(ctx context.Context, db bun.IDB, period time.Time, excludeFromISR bool) (int, float64, float64) {
	start, end := fechaFiltroRange(period)
	noBancarizadas := "'" + strings.Join(FormaPagoNoBancarizadasList(), "','") + "'"
	inversiones := "'" + strings.Join(UsoCFDIInversiones, "','") + "'"
	exclStr := "false"
	if excludeFromISR {
		exclStr = "true"
	}
	q := fmt.Sprintf(`SELECT COUNT(cfdi_pago."UUID"), COALESCE(SUM(pr."ImpPagadoMXN"), 0)::numeric,
		COALESCE(SUM(
			(SELECT COALESCE(SUM((value->>'@ImporteDR')::numeric), 0)
			 FROM jsonb_array_elements(pr."RetencionesDR") AS value
			 WHERE value->>'@ImpuestoDR' = '001')
		), 0)::numeric
		FROM payment_relation pr
		JOIN cfdi cfdi_pago ON pr."UUID" = cfdi_pago."UUID"
		JOIN payment p ON pr.payment_identifier = p.identifier
		JOIN cfdi i ON pr."UUID_related" = i."UUID"
		WHERE cfdi_pago."TipoDeComprobante" = 'P'
		  AND cfdi_pago."Estatus" IS true AND cfdi_pago.is_issued IS true
		  AND p."FormaDePagoP" IN (%s)
		  AND i."UsoCFDIReceptor" NOT IN (%s)
		  AND pr."ExcludeFromISR" IS %s
		  AND pr."FechaPago" >= '%s' AND pr."FechaPago" < '%s'`,
		noBancarizadas, inversiones, exclStr, start, end)
	var count int
	var neto, isr float64
	db.QueryRowContext(ctx, q).Scan(&count, &neto, &isr)
	return count, neto, isr
}

func comprasGastosNoConsiderados(ctx context.Context, db bun.IDB, period time.Time) (int, float64, float64) {
	cI, iI, isrI := noConsideradosIngresosPUE(ctx, db, period, false)
	cP, iP, isrP := noConsideradosPagos(ctx, db, period, false)
	return cI + cP, iI + iP, isrI + isrP
}

func facturasDeEgresosPreLlenadoPagos(ctx context.Context, db bun.IDB, period time.Time, excludeFromISR bool) (int, float64) {
	start, end := fechaFiltroRange(period)
	exclStr := "false"
	if excludeFromISR {
		exclStr = "true"
	}
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM("NetoMXN"), 0)::numeric
		FROM cfdi
		WHERE NOT is_issued AND "TipoDeComprobante" = 'E' AND "Estatus" AND "Version" = '4.0'
		  AND "ExcludeFromISR" = %s
		  AND "FechaFiltro" >= '%s' AND "FechaFiltro" < '%s'`, exclStr, start, end)
	var count int
	var total float64
	db.QueryRowContext(ctx, q).Scan(&count, &total)
	return count, total
}

func adquisicionesDeInversiones(ctx context.Context, db bun.IDB, period time.Time) (int, float64) {
	start, end := fechaFiltroRange(period)
	inversiones := "'" + strings.Join(UsoCFDIInversiones, "','") + "'"
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM("NetoMXN"), 0)::numeric
		FROM cfdi
		WHERE NOT is_issued AND "TipoDeComprobante" = 'I' AND "Estatus" AND "Version" = '4.0'
		  AND "UsoCFDIReceptor" IN (%s)
		  AND "FechaFiltro" >= '%s' AND "FechaFiltro" < '%s'`, inversiones, start, end)
	var count int
	var total float64
	db.QueryRowContext(ctx, q).Scan(&count, &total)
	return count, total
}

// CalcTotalesNominaData computes the full nomina totals table (matching Python's calcular_totales_nomina_data).
func CalcTotalesNominaData(ctx context.Context, tenantDB bun.IDB, companyData map[string]interface{}, period time.Time) map[string]interface{} {
	isrPct := getISRPercentage(companyData)

	conteoNominaGravado, importeNominaGravado := queryGastosNominaGravada(ctx, tenantDB, period)
	importeNominaExento := queryGastosNominaExentoTotal(ctx, tenantDB, period)

	conteoComprasContado, importeComprasContado, isrComprasContado := comprasGastosFacturasContado(ctx, tenantDB, period, false)
	conteoComprasPagos, importeComprasPagos, isrComprasPagos := comprasGastosCFDIPagos(ctx, tenantDB, period, false)

	conteoDevsEgresos, importeDevsEgresos := devDesctosBonifEgresosEmitidos(ctx, tenantDB, period, false)
	conteoDevsIngresos, importeDevsIngresos := devDesctosBonifIngresosEmitidos(ctx, tenantDB, period, false)
	conteoDevsTotal, importeDevsTotal := devPagosProvisionales(ctx, tenantDB, period)

	conteoNCP, importeNCP, isrNCP := noConsideradosIngresosPUE(ctx, tenantDB, period, false)
	conteoNCI, importeNCI, isrNCI := noConsideradosPagos(ctx, tenantDB, period, false)
	conteoCGNC, importeCGNC, isrCGNC := comprasGastosNoConsiderados(ctx, tenantDB, period)

	conteoFacturasRecibidas, importeFacturasRecibidas := facturasDeEgresosPreLlenadoPagos(ctx, tenantDB, period, false)
	conteoAdquisiciones, importeAdquisiciones := adquisicionesDeInversiones(ctx, tenantDB, period)

	deducibleExento := importeNominaExento * isrPct
	totalDeducible := importeNominaGravado + deducibleExento

	// Compras y Gastos
	importeComprasYGastos := importeComprasContado + importeComprasPagos + importeDevsTotal + importeCGNC - importeFacturasRecibidas
	// Deducciones autorizadas
	importeDeduccionesAutorizadas := totalDeducible + importeComprasContado + importeComprasPagos + importeDevsTotal + importeCGNC - importeFacturasRecibidas
	isrDeduccionesAutorizadas := isrComprasContado + isrComprasPagos + isrCGNC

	totalsTable := []map[string]interface{}{
		{"Concepto": "Deducciones autorizadas (sin inversiones)", "Importe": round2(importeDeduccionesAutorizadas), "isr_cargo": round2(isrDeduccionesAutorizadas)},
		{"Concepto": "Compras y gastos", "Importe": round2(importeComprasYGastos)},
		{"Concepto": "Gastos de nomina gravada", "ConteoCFDIs": conteoNominaGravado, "Importe": round2(importeNominaGravado)},
		{"Concepto": "Gastos de nomina exenta", "Importe": round2(importeNominaExento)},
		{"Concepto": "Gastos de nomina exenta deducible", "porcentaje": isrPct, "Importe": round2(deducibleExento)},
		{"Concepto": "Gastos de nomina deducibles", "Importe": round2(totalDeducible)},
		{"Concepto": "Compras y gastos facturas de contado", "ConteoCFDIs": conteoComprasContado, "Importe": round2(importeComprasContado), "isr_cargo": round2(isrComprasContado)},
		{"Concepto": "Compras y gastos CFDI de pagos", "ConteoCFDIs": conteoComprasPagos, "Importe": round2(importeComprasPagos), "isr_cargo": round2(isrComprasPagos)},
		{
			"Concepto":    "Devoluciones, descuentos y bonificaciones facturadas",
			"ConteoCFDIs": conteoDevsTotal, "Importe": round2(importeDevsTotal),
			"concepts": []map[string]interface{}{
				{"Concepto": "Devoluciones, descuentos y bonificaciones en ingresos emitidos", "ConteoCFDIs": conteoDevsIngresos, "Importe": round2(importeDevsIngresos)},
				{"Concepto": "Devoluciones, descuentos y bonificaciones en egresos emitidos", "ConteoCFDIs": conteoDevsEgresos, "Importe": round2(importeDevsEgresos)},
			},
		},
		{
			"Concepto":    "Compras y gastos no considerados en el pre llenado",
			"ConteoCFDIs": conteoCGNC, "Importe": round2(importeCGNC), "isr_cargo": round2(isrCGNC),
			"concepts": []map[string]interface{}{
				{"Concepto": "No considerados en el pre llenado Ingresos PUE", "ConteoCFDIs": conteoNCP, "Importe": round2(importeNCP), "isr_cargo": round2(isrNCP)},
				{"Concepto": "No considerados en el pre llenado Pagos", "ConteoCFDIs": conteoNCI, "Importe": round2(importeNCI), "isr_cargo": round2(isrNCI)},
			},
		},
		{"Concepto": "Facturas de egresos recibidas por compras y gastos", "ConteoCFDIs": conteoFacturasRecibidas, "Importe": round2(importeFacturasRecibidas)},
		{"Concepto": "Adquisiciones por concepto de Inversiones", "ConteoCFDIs": conteoAdquisiciones, "Importe": round2(importeAdquisiciones)},
	}

	// Excluded table
	type exclItem struct {
		name string
		fn   func() int
	}
	exclFn := func(f func(context.Context, bun.IDB, time.Time, bool) (int, float64)) func() int {
		return func() int { c, _ := f(ctx, tenantDB, period, true); return c }
	}
	exclFn3 := func(f func(context.Context, bun.IDB, time.Time, bool) (int, float64, float64)) func() int {
		return func() int { c, _, _ := f(ctx, tenantDB, period, true); return c }
	}
	excluded := []exclItem{
		{"Compras y gastos facturas de contado", exclFn3(comprasGastosFacturasContado)},
		{"Compras y gastos CFDI de pagos", exclFn3(comprasGastosCFDIPagos)},
		{"Devoluciones, descuentos y bonificaciones en ingresos emitidos", exclFn(devDesctosBonifIngresosEmitidos)},
		{"Devoluciones, descuentos y bonificaciones en egresos emitidos", exclFn(devDesctosBonifEgresosEmitidos)},
		{"No considerados en el pre llenado Ingresos PUE", exclFn3(noConsideradosIngresosPUE)},
		{"No considerados en el pre llenado Pagos", exclFn3(noConsideradosPagos)},
		{"Facturas de egresos recibidas por compras y gastos", exclFn(facturasDeEgresosPreLlenadoPagos)},
	}

	totalsTableExcluded := make([]map[string]interface{}, 0, len(excluded))
	for _, e := range excluded {
		totalsTableExcluded = append(totalsTableExcluded, map[string]interface{}{
			"Concepto":    e.name,
			"ConteoCFDIs": e.fn(),
		})
	}

	return map[string]interface{}{
		"totals_table":          totalsTable,
		"totals_table_excluded": totalsTableExcluded,
	}
}

// CalcDeduccionesAutorizadasYCompras computes deductions summary for ISR.
func CalcDeduccionesAutorizadasYCompras(ctx context.Context, db bun.IDB, period time.Time, companyData map[string]interface{}) map[string]interface{} {
	isrPct := getISRPercentage(companyData)

	_, totalGravado := queryGastosNominaGravada(ctx, db, period)
	totalExento := queryGastosNominaExentoTotal(ctx, db, period)
	deducibleExento := totalExento * isrPct
	iNomina := totalGravado + deducibleExento

	_, iContado, isrContado := comprasGastosFacturasContado(ctx, db, period, false)
	_, iPagos, isrPagos := comprasGastosCFDIPagos(ctx, db, period, false)
	_, iDevs := devPagosProvisionales(ctx, db, period)
	_, iNC, isrNC := comprasGastosNoConsiderados(ctx, db, period)
	_, iEgresos := facturasDeEgresosPreLlenadoPagos(ctx, db, period, false)

	return map[string]interface{}{
		"Importe":   round2(iNomina + iContado + iPagos + iDevs + iNC - iEgresos),
		"isr_cargo": round2(isrContado + isrPagos + isrNC),
	}
}

// BuildTotalDeduccionesCFDIQuery executes a dynamic SUM on CFDI fields.
func BuildTotalDeduccionesCFDIQuery(ctx context.Context, db bun.IDB, domain []interface{}, fields []string) map[string]interface{} {
	sums := "COUNT(*) AS \"ConteoCFDIs\""
	for _, f := range fields {
		sums += fmt.Sprintf(`, COALESCE(SUM("%s"), 0) AS "%s"`, f, f)
	}
	q := fmt.Sprintf("SELECT %s FROM cfdi WHERE TRUE", sums)

	domainSQL := buildDomainWhereSQL(domain)
	if domainSQL != "" {
		q += " AND " + domainSQL
	}

	row := db.QueryRowContext(ctx, q)
	vals := make([]interface{}, len(fields)+1)
	ptrs := make([]interface{}, len(fields)+1)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	result := make(map[string]interface{}, len(fields)+1)
	if err := row.Scan(ptrs...); err != nil {
		result["ConteoCFDIs"] = 0
		for _, f := range fields {
			result[f] = 0
		}
		return result
	}
	result["ConteoCFDIs"] = scanInt(vals[0])
	for i, f := range fields {
		result[f] = round2(scanFloat(vals[i+1]))
	}
	return result
}

// BuildTotalDeduccionesPagosQuery executes a dynamic SUM on DoctoRelacionado fields.
func BuildTotalDeduccionesPagosQuery(ctx context.Context, db bun.IDB, domain []interface{}, fields []string) map[string]interface{} {
	sums := "COUNT(*) AS \"ConteoCFDIs\""
	for _, f := range fields {
		sums += fmt.Sprintf(`, COALESCE(SUM("%s"), 0) AS "%s"`, f, f)
	}
	q := fmt.Sprintf("SELECT %s FROM payment_relation WHERE TRUE", sums)

	domainSQL := buildDomainWhereSQL(domain)
	if domainSQL != "" {
		q += " AND " + domainSQL
	}

	row := db.QueryRowContext(ctx, q)
	vals := make([]interface{}, len(fields)+1)
	ptrs := make([]interface{}, len(fields)+1)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	result := make(map[string]interface{}, len(fields)+1)
	if err := row.Scan(ptrs...); err != nil {
		result["ConteoCFDIs"] = 0
		for _, f := range fields {
			result[f] = 0
		}
		return result
	}
	result["ConteoCFDIs"] = scanInt(vals[0])
	for i, f := range fields {
		result[f] = round2(scanFloat(vals[i+1]))
	}
	return result
}

// buildDomainWhereSQL converts domain triples to a WHERE clause.
func buildDomainWhereSQL(domain []interface{}) string {
	var parts []string
	for _, d := range domain {
		triple, ok := d.([]interface{})
		if !ok || len(triple) < 3 {
			continue
		}
		field, _ := triple[0].(string)
		op, _ := triple[1].(string)
		value := triple[2]

		if field == "company_identifier" {
			continue
		}

		col := fmt.Sprintf(`"%s"`, field)
		switch op {
		case "=":
			if value == nil {
				parts = append(parts, fmt.Sprintf("%s IS NULL", col))
			} else {
				parts = append(parts, fmt.Sprintf("%s = '%v'", col, value))
			}
		case "!=":
			if value == nil {
				parts = append(parts, fmt.Sprintf("%s IS NOT NULL", col))
			} else {
				parts = append(parts, fmt.Sprintf("%s != '%v'", col, value))
			}
		case ">", ">=", "<", "<=":
			parts = append(parts, fmt.Sprintf("%s %s '%v'", col, op, value))
		case "ilike":
			parts = append(parts, fmt.Sprintf("%s ILIKE '%v'", col, value))
		case "in":
			if arr, ok := value.([]interface{}); ok {
				vals := make([]string, len(arr))
				for i, v := range arr {
					vals[i] = fmt.Sprintf("'%v'", v)
				}
				parts = append(parts, fmt.Sprintf("%s IN (%s)", col, strings.Join(vals, ",")))
			}
		case "is":
			parts = append(parts, fmt.Sprintf("%s IS %v", col, value))
		case "is not":
			parts = append(parts, fmt.Sprintf("%s IS NOT %v", col, value))
		}
	}
	return strings.Join(parts, " AND ")
}

// Unused but keeps Go compiler happy
var _ = math.Round
