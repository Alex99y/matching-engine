package uuidv7_test

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/uuidv7"
)

func TestNew_NoError(t *testing.T) {
	_, err := uuidv7.New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_IsVersion7(t *testing.T) {
	id, err := uuidv7.New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Version() != 7 {
		t.Errorf("expected UUID version 7, got %d", id.Version())
	}
}

func TestNew_Unique(t *testing.T) {
	id1, err := uuidv7.New()
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	id2, err := uuidv7.New()
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if id1 == id2 {
		t.Error("two consecutive calls must return different UUIDs")
	}
}

func TestNew_LexicographicOrder(t *testing.T) {
	id1, err := uuidv7.New()
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	id2, err := uuidv7.New()
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	// UUIDv7 encodes a millisecond timestamp in the high bits, so
	// string representation is lexicographically monotonic.
	if id1.String() >= id2.String() {
		t.Errorf("expected id1 < id2 lexicographically, got id1=%s id2=%s", id1, id2)
	}
}

func TestFromString_Valid(t *testing.T) {
	id, err := uuidv7.New()
	if err != nil {
		t.Fatalf("unexpected error generating uuid: %v", err)
	}

	parsed, err := uuidv7.FromString(id.String())
	if err != nil {
		t.Fatalf("unexpected error parsing valid uuid: %v", err)
	}
	if parsed != id {
		t.Errorf("parsed uuid does not match original: got %s, want %s", parsed, id)
	}
}

func TestFromString_Invalid(t *testing.T) {
	cases := []string{
		"",
		"not-a-uuid",
		"550e8400-e29b-41d4-a716",
		"xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
	}

	for _, tc := range cases {
		_, err := uuidv7.FromString(tc)
		if err == nil {
			t.Errorf("expected error for input %q, got nil", tc)
		}
	}
}
