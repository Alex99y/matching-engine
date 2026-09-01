package validations

import (
	"errors"
	"fmt"
	"strings"

	validator "github.com/go-playground/validator/v10"
)

// FormatError turns a go-playground validation failure into a message safe to send back to
// the caller, reporting only the first failing field. It returns false for any other error,
// which the caller should treat as an unreadable body rather than a field-level complaint.
func FormatError(err error) (string, bool) {
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) || len(verrs) == 0 {
		return "", false
	}
	return describe(verrs[0]), true
}

func describe(e validator.FieldError) string {
	field := fieldName(e)
	switch e.Tag() {
	case "required":
		return field + " is required"
	case "email":
		return field + " must be a valid email address"
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", field, e.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s characters", field, e.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, strings.ReplaceAll(e.Param(), " ", ", "))
	default:
		return field + " is invalid"
	}
}

func fieldName(e validator.FieldError) string {
	if name := e.Field(); name != "" {
		return name
	}
	return "request"
}
