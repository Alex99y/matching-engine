package repository

import (
	"database/sql"
	"math"
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

// nullU64 must reject a value that overflows int64 instead of silently wrapping it into a
// negative BIGINT — have_quantity/want_quantity are derived from price×qty (see
// DeriveInsertParams in core/internal/orderbook/hydrate.go), so this is reachable from a large
// enough order, not just a theoretical input.
func TestNullU64(t *testing.T) {
	if got, err := nullU64(nil, "have_quantity"); err != nil || got != nil {
		t.Fatalf("nil: got=%v err=%v, want nil, nil", got, err)
	}

	v := uint64(42)
	if got, err := nullU64(&v, "have_quantity"); err != nil || got != int64(42) {
		t.Fatalf("valid positive: got=%v err=%v, want 42, nil", got, err)
	}

	overflow := uint64(math.MaxInt64) + 1
	if got, err := nullU64(&overflow, "have_quantity"); err == nil {
		t.Fatalf("overflowing value accepted: got=%v, want an error", got)
	}
}
