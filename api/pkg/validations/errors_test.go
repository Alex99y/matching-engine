package validations_test

import (
	"errors"
	"testing"

	"github.com/alex99y/matching-engine/api/pkg/validations"
)

type sample struct {
	Username      string `json:"username" validate:"required,min=3,max=25"`
	Email         string `json:"email" validate:"required,email,max=100"`
	Password      string `json:"password" validate:"required,min=6,max=128"`
	Scope         string `json:"scope" validate:"omitempty,oneof=read write"`
	ClientOrderID string `json:"client_order_id" validate:"omitempty,max=64"`
}

func valid() sample {
	return sample{Username: "alice", Email: "alice@example.com", Password: "hunter2!"}
}

func TestFormatErrorDescribesTheFailingField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*sample)
		want   string
	}{
		{"required", func(s *sample) { s.Username = "" }, "username is required"},
		{"min", func(s *sample) { s.Password = "12345" }, "password must be at least 6 characters"},
		{"max", func(s *sample) { s.Username = "aaaaaaaaaaaaaaaaaaaaaaaaaa" }, "username must be at most 25 characters"},
		{"email", func(s *sample) { s.Email = "alice-at-example" }, "email must be a valid email address"},
		{"oneof", func(s *sample) { s.Scope = "admin" }, "scope must be one of: read, write"},
	}

	v := validations.NewStructValidator()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := valid()
			tc.mutate(&s)

			msg, ok := validations.FormatError(v.Validate(s))
			if !ok {
				t.Fatalf("FormatError did not recognise a validation failure")
			}
			if msg != tc.want {
				t.Fatalf("msg = %q, want %q", msg, tc.want)
			}
		})
	}
}

// The caller sent client_order_id, so that is the name it must be told about — lowercasing
// the Go field would have produced "clientorderid".
func TestFormatErrorNamesFieldsAsTheCallerSentThem(t *testing.T) {
	s := valid()
	s.ClientOrderID = string(make([]byte, 65))

	msg, ok := validations.FormatError(validations.NewStructValidator().Validate(s))
	if !ok {
		t.Fatalf("FormatError did not recognise a validation failure")
	}
	if msg != "client_order_id must be at most 64 characters" {
		t.Fatalf("msg = %q, want the json field name", msg)
	}
}

func TestFormatErrorIgnoresNonValidationErrors(t *testing.T) {
	for _, err := range []error{errors.New("unexpected end of JSON input"), nil} {
		if msg, ok := validations.FormatError(err); ok {
			t.Fatalf("FormatError claimed %v was a validation failure: %q", err, msg)
		}
	}
}

func TestValidStructProducesNoError(t *testing.T) {
	if err := validations.NewStructValidator().Validate(valid()); err != nil {
		t.Fatalf("a valid struct was rejected: %v", err)
	}
}
