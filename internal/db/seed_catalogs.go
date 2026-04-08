package db

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/csv"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

//go:embed seeddata/*.csv
var seedCatalogCSV embed.FS

// Order matches Python Alembic data migrations (bef098e1f688 + 84c3a8e301b6); stable sorted.
var catalogMainTables = []string{
	"cat_aduana",
	"cat_clave_prod_serv",
	"cat_clave_unidad",
	"cat_exportacion",
	"cat_forma_pago",
	"cat_impuesto",
	"cat_meses",
	"cat_metodo_pago",
	"cat_moneda",
	"cat_objeto_imp",
	"cat_pais",
	"cat_periodicidad",
	"cat_regimen_fiscal",
	"cat_tipo_de_comprobante",
	"cat_tipo_relacion",
	"cat_uso_cfdi",
}

var catalogNominaTables = []string{
	"cat_nom_banco",
	"cat_nom_clave_ent_fed",
	"cat_nom_periodicidad_pago",
	"cat_nom_riesgo_puesto",
	"cat_nom_tipo_contrato",
	"cat_nom_tipo_jornada",
	"cat_nom_tipo_nomina",
	"cat_nom_tipo_regimen",
}

var catalogSeedWhitelist map[string]struct{}

func init() {
	catalogSeedWhitelist = make(map[string]struct{})
	for _, t := range catalogMainTables {
		catalogSeedWhitelist[t] = struct{}{}
	}
	for _, t := range catalogNominaTables {
		catalogSeedWhitelist[t] = struct{}{}
	}
}

// SeedCatalogs replaces catalog rows with embedded CSV data (same as legacy Alembic bulk_insert).
// tx must be an open transaction; caller commits or rolls back.
func SeedCatalogs(ctx context.Context, tx *sql.Tx) error {
	start := time.Now()
	for _, tbl := range catalogMainTables {
		if err := seedCatalogTable(ctx, tx, tbl); err != nil {
			return err
		}
	}
	for _, tbl := range catalogNominaTables {
		if err := seedCatalogTable(ctx, tx, tbl); err != nil {
			return err
		}
	}
	slog.Info("catalog seed completed", "tables", len(catalogMainTables)+len(catalogNominaTables), "elapsed", time.Since(start))
	return nil
}

func seedCatalogTable(ctx context.Context, tx *sql.Tx, table string) error {
	if _, ok := catalogSeedWhitelist[table]; !ok {
		return fmt.Errorf("seed: unknown catalog table %q", table)
	}
	path := "seeddata/" + table + ".csv"
	raw, err := seedCatalogCSV.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = '|'
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if len(records) < 1 {
		return fmt.Errorf("seed %s: empty csv", table)
	}
	// Skip header (code|name)
	data := records[1:]
	if len(data) == 0 {
		slog.Warn("catalog csv has header only", "table", table)
	}

	del := fmt.Sprintf(`DELETE FROM public.%s`, table)
	if _, err := tx.ExecContext(ctx, del); err != nil {
		return fmt.Errorf("delete %s: %w", table, err)
	}

	const batchSize = 500
	for i := 0; i < len(data); i += batchSize {
		end := i + batchSize
		if end > len(data) {
			end = len(data)
		}
		if err := insertCatalogBatch(ctx, tx, table, data[i:end]); err != nil {
			return fmt.Errorf("insert %s batch %d-%d: %w", table, i, end, err)
		}
	}
	slog.Info("catalog table seeded", "table", table, "rows", len(data))
	return nil
}

func insertCatalogBatch(ctx context.Context, tx *sql.Tx, table string, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	var sb strings.Builder
	args := make([]any, 0, len(rows)*2)
	sb.WriteString(fmt.Sprintf(`INSERT INTO public.%s (code, name) VALUES `, table))
	for i, row := range rows {
		if len(row) < 2 {
			return fmt.Errorf("row %d: expected code|name", i)
		}
		if i > 0 {
			sb.WriteByte(',')
		}
		n := len(args)
		sb.WriteString(fmt.Sprintf("($%d,$%d)", n+1, n+2))
		args = append(args, row[0], row[1])
	}
	_, err := tx.ExecContext(ctx, sb.String(), args...)
	return err
}
