package utils

import "database/sql"

// NullInt64ToUint64 converts a nullable BIGINT scan target to *uint64, preserving NULL as nil
// instead of collapsing it to zero.
func NullInt64ToUint64(v sql.NullInt64) *uint64 {
	if !v.Valid {
		return nil
	}
	val := uint64(v.Int64)
	return &val
}
