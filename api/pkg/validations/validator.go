package validations

import (
	"reflect"
	"strings"

	validator "github.com/go-playground/validator/v10"
)

type structValidator struct {
	validate *validator.Validate
}

func (v *structValidator) Validate(out any) error {
	return v.validate.Struct(out)
}

func NewStructValidator() *structValidator {
	validate := validator.New()
	// Report the field by the name the caller sent, not the Go field it landed in — the
	// difference matters for anything that is not a single lowercase word (client_order_id).
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
	return &structValidator{validate: validate}
}
