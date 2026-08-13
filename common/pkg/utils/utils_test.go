package utils_test

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

func TestStringToInt(t *testing.T) {
	got, err := utils.StringToInt("42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestStringToIntInvalid(t *testing.T) {
	if _, err := utils.StringToInt("not-a-number"); err == nil {
		t.Error("expected an error for a non-integer input")
	}
}

func TestFormatUint64(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0"},
		{42, "42"},
		{18446744073709551615, "18446744073709551615"}, // math.MaxUint64
	}
	for _, c := range cases {
		if got := utils.FormatUint64(c.in); got != c.want {
			t.Errorf("FormatUint64(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
