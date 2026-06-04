package validations

import (
	validator "github.com/go-playground/validator/v10"
)

type structValidator struct {
	validate *validator.Validate
}

func (v *structValidator) Validate(out any) error {
	return v.validate.Struct(out)
}

func NewStructValidator() *structValidator {
	return &structValidator{validate: validator.New()}
}
