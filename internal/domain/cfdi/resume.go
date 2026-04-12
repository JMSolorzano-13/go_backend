package cfdi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

const (
	ResumeBasic             = "BASIC"
	ResumeN                 = "N"
	ResumeP                 = "P"
	ResumePaymentWithDoctos = "PAYMENT_WITH_DOCTOS"

	// resumeLongSpanMinDays: when the filtered FechaFiltro window is at least this many days,
	// treat "Acumulado" the same as "Periodo" (no widening to Jan 1). Matches product expectation
	// for full-year (and year-shaped) ranges where the UI lower bound may start after Jan 1.
	resumeLongSpanMinDays = 300
)

// #region agent log
const debugResumeLogPath = "/Users/juanmanuelsolorzano/Developer/ez/local_siigo_fiscal/.cursor/debug-921a30.log"

func debugResumeNDJSON(payload map[string]interface{}) {
	payload["sessionId"] = "921a30"
	payload["timestamp"] = time.Now().UnixMilli()
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	f, err := os.OpenFile(debugResumeLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
}

// #endregion

var resumeFieldsBasic = []string{
	`SUM("SubTotalMXN") AS "SubTotalMXN"`,
	`SUM("SubTotal") AS "SubTotal"`,
	`SUM("NetoMXN") AS "NetoMXN"`,
	`SUM("Neto") AS "Neto"`,
	`SUM("TrasladosIVAMXN") AS "TrasladosIVAMXN"`,
	`SUM("TrasladosIVA") AS "TrasladosIVA"`,
	`SUM("TrasladosIEPSMXN") AS "TrasladosIEPSMXN"`,
	`SUM("TrasladosIEPS") AS "TrasladosIEPS"`,
	`SUM("TrasladosISRMXN") AS "TrasladosISRMXN"`,
	`SUM("TrasladosISR") AS "TrasladosISR"`,
	`SUM("RetencionesIVAMXN") AS "RetencionesIVAMXN"`,
	`SUM("RetencionesIVA") AS "RetencionesIVA"`,
	`SUM("RetencionesIEPSMXN") AS "RetencionesIEPSMXN"`,
	`SUM("RetencionesIEPS") AS "RetencionesIEPS"`,
	`SUM("RetencionesISRMXN") AS "RetencionesISRMXN"`,
	`SUM("RetencionesISR") AS "RetencionesISR"`,
	`SUM("TotalMXN") AS "TotalMXN"`,
	`SUM("Total") AS "Total"`,
	`SUM("DescuentoMXN") AS "DescuentoMXN"`,
	`SUM("Descuento") AS "Descuento"`,
	`SUM(COALESCE("RetencionesIVA",0) + COALESCE("RetencionesIEPS",0) + COALESCE("RetencionesISR",0)) AS "ImpuestosRetenidos"`,
	`SUM("pr_count") AS "PaymentRelatedCount"`,
	`COUNT(*) AS "count"`,
}

var resumeFieldsN = []string{
	`COUNT(*) AS "Qty"`,
	`COUNT(DISTINCT "RfcReceptor") AS "EmpleadosQty"`,
	`SUM(n."TotalPercepciones") AS "TotalPercepciones"`,
	`SUM(n."TotalDeducciones") AS "TotalDeducciones"`,
	`SUM(n."TotalOtrosPagos") AS "TotalOtrosPagos"`,
	`SUM(n."PercepcionesTotalSueldos") AS "PercepcionesTotalSueldos"`,
	`SUM(n."PercepcionesTotalGravado") AS "PercepcionesTotalGravado"`,
	`SUM(n."PercepcionesTotalExento") AS "PercepcionesTotalExento"`,
	`SUM(n."DeduccionesTotalImpuestosRetenidos") AS "DeduccionesTotalImpuestosRetenidos"`,
	`SUM(n."DeduccionesTotalOtrasDeducciones") AS "DeduccionesTotalOtrasDeducciones"`,
	`SUM(n."SubsidioCausado") AS "SubsidioCausado"`,
	`SUM(COALESCE(n."TotalPercepciones",0) + COALESCE(n."TotalOtrosPagos",0) - COALESCE(n."TotalDeducciones",0)) AS "NetoAPagar"`,
	`SUM(COALESCE(n."TotalPercepciones",0) - COALESCE(n."PercepcionesTotalSueldos",0)) AS "OtrasPercepciones"`,
	`SUM(n."AjusteISRRetenido") AS "AjusteISRRetenido"`,
	`SUM(n."PercepcionesJubilacionPensionRetiro") AS "PercepcionesJubilacionPensionRetiro"`,
	`SUM(n."PercepcionesSeparacionIndemnizacion") AS "PercepcionesSeparacionIndemnizacion"`,
}

var resumeFieldsP = []string{
	`COUNT(*) AS "count"`,
	`SUM("BaseIVA16") AS "BaseIVA16"`,
	`SUM("IVATrasladado16") AS "IVATrasladado16"`,
	`SUM("BaseIVA8") AS "BaseIVA8"`,
	`SUM("IVATrasladado8") AS "IVATrasladado8"`,
	`SUM("BaseIVA0") AS "BaseIVA0"`,
	`SUM(0) AS "IVATrasladado0"`,
	`SUM("BaseIVAExento") AS "BaseIVAExento"`,
	`SUM("TrasladosIVA") AS "TrasladosIVA"`,
	`SUM("RetencionesIVA") AS "RetencionesIVA"`,
	`SUM("RetencionesISR") AS "RetencionesISR"`,
	`SUM("RetencionesIEPS") AS "RetencionesIEPS"`,
	`SUM("Total") AS "Total"`,
	`SUM("pr_count") AS "PaymentRelatedCount"`,
}

func getResumeFields(resumeType string, paymentsInDomain bool) []string {
	if paymentsInDomain {
		return resumeFieldsP
	}
	switch resumeType {
	case ResumeN:
		return resumeFieldsN
	case ResumeP:
		return resumeFieldsP
	default:
		return resumeFieldsBasic
	}
}

func hasPaymentsInDomain(domain []interface{}) bool {
	for _, d := range domain {
		if triple, ok := d.([]interface{}); ok && len(triple) >= 1 {
			if field, ok := triple[0].(string); ok && field == "payments.FormaDePagoP" {
				return true
			}
		}
	}
	return false
}

func isHistoricDomain(domain []interface{}) bool {
	for _, d := range domain {
		if triple, ok := d.([]interface{}); ok && len(triple) >= 1 {
			if field, ok := triple[0].(string); ok && field == "FechaFiltro" {
				return false
			}
		}
	}
	return true
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func appendFuzzyClause(q, fuzzySearch string) string {
	if fuzzySearch == "" {
		return q
	}
	fs := escapeSQLString(fuzzySearch)
	fuzzyFields := []string{`"NombreEmisor"`, `"NombreReceptor"`, `"RfcEmisor"`, `"RfcReceptor"`, `"UUID"`}
	concat := `CONCAT(` + strings.Join(fuzzyFields, `,' ',`) + `)`
	return q + fmt.Sprintf(` AND unaccent(%s) ILIKE unaccent('%%%s%%')`, concat, fs)
}

func parseDomainTime(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		s := strings.TrimSpace(t)
		layouts := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05.000",
			"2006-01-02T15:04:05",
			"2006-01-02",
		}
		for _, l := range layouts {
			if tt, err := time.Parse(l, s); err == nil {
				return tt, true
			}
		}
		if strings.HasSuffix(s, "Z") {
			if tt, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return tt, true
			}
		}
	case []byte:
		return parseDomainTime(string(t))
	}
	return time.Time{}, false
}

func collectFechaFiltroLowerStarts(domain []interface{}) []time.Time {
	var starts []time.Time
	for _, d := range domain {
		triple, ok := d.([]interface{})
		if !ok || len(triple) < 3 {
			continue
		}
		field, _ := triple[0].(string)
		op, _ := triple[1].(string)
		if field != "FechaFiltro" || (op != ">" && op != ">=") {
			continue
		}
		if ts, ok2 := parseDomainTime(triple[2]); ok2 {
			starts = append(starts, ts)
		}
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
	return starts
}

// earliestFechaFiltroUpper returns the tightest upper bound from FechaFiltro with op "<" or "<=".
func earliestFechaFiltroUpper(domain []interface{}) (time.Time, bool) {
	var best time.Time
	var found bool
	for _, d := range domain {
		triple, ok := d.([]interface{})
		if !ok || len(triple) < 3 {
			continue
		}
		field, _ := triple[0].(string)
		op, _ := triple[1].(string)
		if field != "FechaFiltro" || (op != "<" && op != "<=") {
			continue
		}
		ts, ok2 := parseDomainTime(triple[2])
		if !ok2 {
			continue
		}
		if !found || ts.Before(best) {
			best, found = ts, true
		}
	}
	return best, found
}

// longResumeWindowSkipsExerciseWiden is true when the filtered date window is long enough that
// widening the lower bound to Jan 1 would mis-label the row as "year cumulative" while the UI
// already represents a full-year (or near-year) slice — Periodo and Acumulado must match.
func longResumeWindowSkipsExerciseWiden(domain []interface{}) bool {
	starts := collectFechaFiltroLowerStarts(domain)
	if len(starts) == 0 {
		return false
	}
	hi, ok := earliestFechaFiltroUpper(domain)
	if !ok {
		return false
	}
	lo := starts[0]
	if !hi.After(lo) {
		return false
	}
	days := int(hi.Sub(lo).Hours() / 24)
	return days >= resumeLongSpanMinDays
}

func stripFechaFiltroLowerBounds(domain []interface{}) []interface{} {
	newDomain := make([]interface{}, 0, len(domain)+1)
	for _, d := range domain {
		triple, ok := d.([]interface{})
		if !ok || len(triple) < 2 {
			newDomain = append(newDomain, d)
			continue
		}
		field, _ := triple[0].(string)
		op, _ := triple[1].(string)
		if field == "FechaFiltro" && (op == ">" || op == ">=") {
			continue
		}
		newDomain = append(newDomain, d)
	}
	return newDomain
}

// domainCopyForExercise mirrors Python _get_domain_with_normalized_FechaFiltro_begin_exercise:
// drop lower FechaFiltro bounds, then add FechaFiltro >= Jan 1 of the exercise year derived from
// the earliest explicit lower bound, else from MAX(FechaFiltro) when provided.
func domainCopyForExercise(domain []interface{}, maxFecha sql.NullTime) []interface{} {
	starts := collectFechaFiltroLowerStarts(domain)
	newDomain := stripFechaFiltroLowerBounds(domain)

	var anchor time.Time
	var have bool
	if len(starts) > 0 {
		anchor, have = starts[0], true
	} else if maxFecha.Valid {
		anchor, have = maxFecha.Time, true
	}
	if !have {
		return newDomain
	}
	y := anchor.UTC().Year()
	jan1 := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
	newDomain = append(newDomain, []interface{}{"FechaFiltro", ">=", jan1.Format("2006-01-02T15:04:05")})
	return newDomain
}

func queryMaxFechaFiltro(ctx context.Context, db bun.IDB, domain []interface{}, fuzzySearch string) (sql.NullTime, error) {
	domainWhere := buildDomainWhereSQL(domain)
	where := "TRUE"
	if domainWhere != "" {
		where = domainWhere
	}
	q := appendFuzzyClause(fmt.Sprintf(`SELECT MAX("FechaFiltro") FROM cfdi WHERE %s`, where), fuzzySearch)
	var nt sql.NullTime
	err := db.QueryRowContext(ctx, q).Scan(&nt)
	return nt, err
}

// ComputeResume calculates aggregated resume data for CFDIs matching the domain.
func ComputeResume(ctx context.Context, db bun.IDB, domain []interface{}, fuzzySearch string, resumeType string) map[string]interface{} {
	paymentsInDomain := hasPaymentsInDomain(domain)
	fields := getResumeFields(resumeType, paymentsInDomain)

	needJoin := resumeType == ResumeN

	fromClause := "cfdi"
	if needJoin {
		fromClause = `cfdi JOIN nomina n ON cfdi."UUID" = n.cfdi_uuid`
	}

	fieldStr := strings.Join(fields, ", ")
	domainWhere := buildDomainWhereSQL(domain)
	whereClause := "TRUE"
	if domainWhere != "" {
		whereClause = domainWhere
	}

	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s", fieldStr, fromClause, whereClause)
	q = appendFuzzyClause(q, fuzzySearch)

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return map[string]interface{}{}
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	if !rows.Next() {
		return map[string]interface{}{}
	}

	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return map[string]interface{}{}
	}
	if err := rows.Err(); err != nil {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{}, len(cols)+1)
	for i, col := range cols {
		result[col] = normalizeResumeValue(vals[i])
	}

	result["total_docto_relacionados"] = getTotalDoctoRelacionados(ctx, db, domain, fuzzySearch, paymentsInDomain)
	return result
}

func getTotalDoctoRelacionados(ctx context.Context, db bun.IDB, domain []interface{}, fuzzySearch string, paymentsInDomain bool) float64 {
	domainWhere := buildDomainWhereSQL(domain)
	where := "TRUE"
	if domainWhere != "" {
		where = domainWhere
	}

	q := fmt.Sprintf(`SELECT COALESCE(SUM(pr."ImpPagadoMXN"), 0)
		FROM payment_relation pr
		JOIN cfdi ON pr."UUID" = cfdi."UUID" AND cfdi."Version" = '4.0'
		WHERE %s`, where)

	var total float64
	db.QueryRowContext(ctx, q).Scan(&total)
	return total
}

func normalizeResumeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case float64:
		return round2(val)
	case int64:
		return val
	case []byte:
		var f float64
		fmt.Sscanf(string(val), "%f", &f)
		return round2(f)
	case nil:
		return 0
	default:
		return val
	}
}

// CFDIResume executes the full resume logic (filtered + exercise).
func CFDIResume(ctx context.Context, db bun.IDB, domain []interface{}, fuzzySearch string, resumeType string) map[string]interface{} {
	filtered := ComputeResume(ctx, db, domain, fuzzySearch, resumeType)
	var exercise map[string]interface{}
	if isHistoricDomain(domain) {
		exercise = filtered
	} else {
		longSpan := longResumeWindowSkipsExerciseWiden(domain)
		// #region agent log
		starts := collectFechaFiltroLowerStarts(domain)
		hi, hiOK := earliestFechaFiltroUpper(domain)
		days := 0
		if len(starts) > 0 && hiOK && hi.After(starts[0]) {
			days = int(hi.Sub(starts[0]).Hours() / 24)
		}
		debugResumeNDJSON(map[string]interface{}{
			"hypothesisId":      "H-longSpan",
			"location":          "resume.go:CFDIResume",
			"message":           "resume window evaluation",
			"longSpanSkipWiden": longSpan,
			"fechaLowerCount":   len(starts),
			"fechaUpperOk":      hiOK,
			"approxSpanDays":    days,
			"resumeType":        resumeType,
			"filteredCount":     filtered["count"],
		})
		// #endregion
		if longSpan {
			exercise = filtered
		} else {
			maxFecha, _ := queryMaxFechaFiltro(ctx, db, domain, fuzzySearch)
			exerciseDomain := domainCopyForExercise(domain, maxFecha)
			exercise = ComputeResume(ctx, db, exerciseDomain, fuzzySearch, resumeType)
			if len(exercise) == 0 {
				exercise = filtered
			}
		}
		// #region agent log
		debugResumeNDJSON(map[string]interface{}{
			"hypothesisId":  "H-outcome",
			"location":      "resume.go:CFDIResume",
			"message":       "resume counts after exercise branch",
			"longSpanUsed":  longSpan,
			"filteredCount": filtered["count"],
			"exerciseCount": exercise["count"],
		})
		// #endregion
	}
	return map[string]interface{}{
		"filtered":  filtered,
		"excercise": exercise, // intentional misspelling: matches Python backend + frontend
	}
}

// CountCFDIsByType returns count grouped by TipoDeComprobante.
func CountCFDIsByType(ctx context.Context, db bun.IDB, domain []interface{}, fuzzySearch string) map[string]string {
	domainWhere := buildDomainWhereSQL(domain)
	where := "TRUE"
	if domainWhere != "" {
		where = domainWhere
	}

	q := fmt.Sprintf(`SELECT "TipoDeComprobante", COUNT(DISTINCT "UUID") FROM cfdi WHERE %s GROUP BY "TipoDeComprobante"`, where)

	if fuzzySearch != "" {
		fuzzyFields := []string{`"NombreEmisor"`, `"NombreReceptor"`, `"RfcEmisor"`, `"RfcReceptor"`, `"UUID"`}
		concat := `CONCAT(` + strings.Join(fuzzyFields, `,' ',`) + `)`
		q = fmt.Sprintf(`SELECT "TipoDeComprobante", COUNT(DISTINCT "UUID") FROM cfdi WHERE %s AND unaccent(%s) ILIKE unaccent('%%%s%%') GROUP BY "TipoDeComprobante"`, where, concat, fuzzySearch)
	}

	result := map[string]string{"E": "0", "I": "0", "N": "0", "P": "0", "T": "0"}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var tipo string
		var count int64
		rows.Scan(&tipo, &count)
		result[tipo] = fmt.Sprintf("%d", count)
	}
	return result
}

// GetByPeriod groups CFDIs by year/month.
func GetByPeriod(ctx context.Context, db bun.IDB, domain []interface{}) map[string]map[string]map[string]interface{} {
	domainWhere := buildDomainWhereSQL(domain)
	where := `"TipoDeComprobante" = 'I' AND "Estatus"`
	if domainWhere != "" {
		where += " AND " + domainWhere
	}

	result := make(map[string]map[string]map[string]interface{})

	for _, moveType := range []struct {
		name   string
		filter string
	}{
		{"incomes", `is_issued`},
		{"expenses", `NOT is_issued`},
	} {
		q := fmt.Sprintf(`SELECT EXTRACT(YEAR FROM "FechaFiltro")::int, EXTRACT(MONTH FROM "FechaFiltro")::int,
			COUNT(*), COALESCE(SUM("TotalMXN"),0), COALESCE(SUM("SubTotalMXN"),0), COALESCE(SUM("NetoMXN"),0)
			FROM cfdi WHERE %s AND %s
			GROUP BY 1, 2 ORDER BY 1, 2`, where, moveType.filter)

		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			continue
		}
		for rows.Next() {
			var year, month, count int
			var total, subtotal, neto float64
			rows.Scan(&year, &month, &count, &total, &subtotal, &neto)
			period := fmt.Sprintf("%04d-%02d", year, month)
			if _, ok := result[period]; !ok {
				result[period] = make(map[string]map[string]interface{})
			}
			result[period][moveType.name] = map[string]interface{}{
				"count":    count,
				"total":    total,
				"subtotal": subtotal,
				"neto":     neto,
			}
		}
		rows.Close()
	}
	return result
}
