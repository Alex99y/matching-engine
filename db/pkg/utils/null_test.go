package utils_test

import (
	"database/sql"
	"testing"

	"github.com/alex99y/matching-engine/db/pkg/utils"
)

func TestNullInt64ToUint64Null(t *testing.T) {
	if got := utils.NullInt64ToUint64(sql.NullInt64{}); got != nil {
		t.Errorf("NULL: got %v, want nil", got)
	}
}

func TestNullInt64ToUint64Zero(t *testing.T) {
	// A valid 0 must stay distinguishable from NULL — that's the whole point of returning
	// a pointer instead of a bare uint64.
	got := utils.NullInt64ToUint64(sql.NullInt64{Valid: true, Int64: 0})
	if got == nil {
		t.Fatal("valid 0: got nil, want a pointer to 0")
	}
	if *got != 0 {
		t.Errorf("valid 0: got %d, want 0", *got)
	}
}

func TestNullInt64ToUint64Value(t *testing.T) {
	got := utils.NullInt64ToUint64(sql.NullInt64{Valid: true, Int64: 42})
	if got == nil {
		t.Fatal("valid 42: got nil, want a pointer to 42")
	}
	if *got != 42 {
		t.Errorf("valid 42: got %d, want 42", *got)
	}
}
