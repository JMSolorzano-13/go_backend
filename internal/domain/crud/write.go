package crud

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/uptrace/bun"
)

// NewIdentifier generates a random UUID v4 string for the identifier field.
func NewIdentifier() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Create inserts a single record and returns it.
func Create[T any](ctx context.Context, idb bun.IDB, record *T) error {
	_, err := idb.NewInsert().Model(record).Exec(ctx)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	return nil
}

// GetByIdentifiers fetches records whose `identifier` column is in the given set.
func GetByIdentifiers[T any](ctx context.Context, idb bun.IDB, ids []string) ([]T, error) {
	var records []T
	err := idb.NewSelect().
		Model(&records).
		Where("identifier IN (?)", bun.In(ids)).
		OrderExpr(`"identifier" ASC`).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get by identifiers: %w", err)
	}
	if len(records) != len(ids) {
		return records, fmt.Errorf("expected %d records, found %d", len(ids), len(records))
	}
	return records, nil
}

// GetByID fetches a single record by its numeric ID.
func GetByID[T any](ctx context.Context, idb bun.IDB, id int64) (*T, error) {
	record := new(T)
	err := idb.NewSelect().
		Model(record).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get by id: %w", err)
	}
	return record, nil
}

// Update modifies the given columns on records matching the identifiers.
func Update[T any](ctx context.Context, idb bun.IDB, ids []string, values map[string]interface{}) ([]T, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(values) == 0 {
		return GetByIdentifiers[T](ctx, idb, ids)
	}

	q := idb.NewUpdate().Model((*T)(nil)).Where("identifier IN (?)", bun.In(ids))
	for col, val := range values {
		q = q.Set(fmt.Sprintf(`"%s" = ?`, col), val)
	}
	_, err := q.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}

	return GetByIdentifiers[T](ctx, idb, ids)
}

// Delete removes records by their identifiers and returns the deleted IDs.
func Delete[T any](ctx context.Context, idb bun.IDB, ids []string) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	records, err := GetByIdentifiers[T](ctx, idb, ids)
	if err != nil {
		return nil, err
	}

	_, err = idb.NewDelete().
		Model((*T)(nil)).
		Where("identifier IN (?)", bun.In(ids)).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("delete: %w", err)
	}

	deletedIDs := make([]int64, 0, len(records))
	for _, r := range records {
		m := SerializeOne(r)
		if id, ok := m["id"].(float64); ok {
			deletedIDs = append(deletedIDs, int64(id))
		}
	}
	return deletedIDs, nil
}

// RestrictedCreateFields are fields that cannot be set manually during create.
var RestrictedCreateFields = map[string]bool{
	"id":         true,
	"created_at": true,
	"updated_at": true,
}

// RestrictedUpdateFields are fields that cannot be updated.
var RestrictedUpdateFields = map[string]bool{
	"id":         true,
	"created_at": true,
	"updated_at": true,
}

// ValidateCreateData checks that no restricted fields are being set.
func ValidateCreateData(data map[string]interface{}) error {
	for key := range data {
		if RestrictedCreateFields[key] {
			return fmt.Errorf("the field '%s' cannot be set manually", key)
		}
	}
	return nil
}

// ValidateUpdateData checks that no restricted fields are being updated.
func ValidateUpdateData(data map[string]interface{}) error {
	for key := range data {
		if RestrictedUpdateFields[key] {
			return fmt.Errorf("the field '%s' cannot be updated manually", key)
		}
	}
	return nil
}
