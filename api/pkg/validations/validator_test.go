package validations_test

import (
	"testing"

	"github.com/alex99y/matching-engine/api/pkg/validations"
)

type testPayload struct {
	Name string `validate:"required"`
	Age  int    `validate:"gte=0"`
}

func TestStructValidatorValid(t *testing.T) {
	v := validations.NewStructValidator()
	if err := v.Validate(&testPayload{Name: "alex", Age: 30}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStructValidatorInvalid(t *testing.T) {
	v := validations.NewStructValidator()
	cases := []testPayload{
		{Name: "", Age: 30},     // missing required field
		{Name: "alex", Age: -1}, // fails gte=0
	}
	for _, c := range cases {
		if err := v.Validate(&c); err == nil {
			t.Errorf("Validate(%+v): expected an error", c)
		}
	}
}
