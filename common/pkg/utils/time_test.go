package utils_test

import (
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

func TestParseUnixTimestamp(t *testing.T) {
	got, err := utils.ParseUnixTimestamp("1700000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Unix(1700000000, 0).UTC()
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Errorf("expected UTC location, got %v", got.Location())
	}
}

func TestParseUnixTimestampZero(t *testing.T) {
	got, err := utils.ParseUnixTimestamp("0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(time.Unix(0, 0).UTC()) {
		t.Errorf("got %v, want unix epoch", got)
	}
}

func TestParseUnixTimestampInvalid(t *testing.T) {
	cases := []string{"", "not-a-number", "1700000000.5", "  1700000000"}
	for _, raw := range cases {
		if _, err := utils.ParseUnixTimestamp(raw); err == nil {
			t.Errorf("ParseUnixTimestamp(%q): expected an error", raw)
		}
	}
}
