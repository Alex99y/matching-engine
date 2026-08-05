package repository

import (
	"database/sql"
	"testing"
)

// safeUint64 must reject a negative value instead of silently wrapping it into a huge unsigned
// quantity, and must treat SQL NULL as 0 — matching the pre-existing behavior of a bare
// uint64(NullInt64.Int64) cast on a NULL field (the one legitimate case: the unset side of a
// market order).
func TestSafeUint64(t *testing.T) {
	if got, err := safeUint64(sql.NullInt64{}, "have_quantity"); err != nil || got != 0 {
		t.Fatalf("NULL: got=%d err=%v, want 0, nil", got, err)
	}
	if got, err := safeUint64(sql.NullInt64{Valid: true, Int64: 42}, "have_quantity"); err != nil || got != 42 {
		t.Fatalf("valid positive: got=%d err=%v, want 42, nil", got, err)
	}
	if got, err := safeUint64(sql.NullInt64{Valid: true, Int64: -1}, "have_quantity"); err == nil {
		t.Fatalf("negative value accepted: got=%d, want an error", got)
	}
}
