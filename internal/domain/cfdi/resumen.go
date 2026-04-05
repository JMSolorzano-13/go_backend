package cfdi

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

type IngresosNominales struct {
	Vigentes     int     `json:"vigentes"`
	Cancelados   int     `json:"cancelados"`
	SubtotalMXN  float64 `json:"subtotal_mxn"`
	DescuentoMXN float64 `json:"descuento_mxn"`
}

type ResumenResult struct {
	Datos map[int]IngresosNominales `json:"datos"`
	Total IngresosNominales         `json:"total"`
}

// EmitidosIngresosAnioMesResumen calculates the monthly breakdown of issued income CFDIs.
func EmitidosIngresosAnioMesResumen(ctx context.Context, db bun.IDB, anio, mes int) *ResumenResult {
	start := time.Date(anio, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(anio, time.Month(mes)+1, 0, 0, 0, 0, 0, time.UTC)

	q := fmt.Sprintf(`SELECT
		EXTRACT(MONTH FROM "FechaFiltro")::int AS mes,
		COUNT(CASE WHEN "Estatus" THEN 1 END) AS vigentes,
		COUNT(CASE WHEN NOT "Estatus" THEN 1 END) AS cancelados,
		COALESCE(SUM(CASE WHEN "Estatus" THEN COALESCE("SubTotalMXN", 0) END), 0) AS subtotal_mxn,
		COALESCE(SUM(CASE WHEN "Estatus" THEN COALESCE("DescuentoMXN", 0) END), 0) AS descuento_mxn
		FROM cfdi
		WHERE is_issued AND "TipoDeComprobante" = 'I'
		  AND "FechaFiltro" BETWEEN '%s' AND '%s'
		GROUP BY mes`, fmtDate(start), fmtDate(endDate))

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return emptyResumen(mes)
	}
	defer rows.Close()

	datos := make(map[int]IngresosNominales)
	for rows.Next() {
		var m int
		var ing IngresosNominales
		rows.Scan(&m, &ing.Vigentes, &ing.Cancelados, &ing.SubtotalMXN, &ing.DescuentoMXN)
		datos[m] = ing
	}

	// Fill missing months
	for i := 1; i <= mes; i++ {
		if _, ok := datos[i]; !ok {
			datos[i] = IngresosNominales{}
		}
	}

	total := IngresosNominales{}
	for _, v := range datos {
		total.Vigentes += v.Vigentes
		total.Cancelados += v.Cancelados
		total.SubtotalMXN += v.SubtotalMXN
		total.DescuentoMXN += v.DescuentoMXN
	}

	return &ResumenResult{Datos: datos, Total: total}
}

func emptyResumen(mes int) *ResumenResult {
	datos := make(map[int]IngresosNominales)
	for i := 1; i <= mes; i++ {
		datos[i] = IngresosNominales{}
	}
	return &ResumenResult{Datos: datos, Total: IngresosNominales{}}
}
