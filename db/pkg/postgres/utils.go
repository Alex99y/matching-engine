package postgres

import (
	"errors"

	"github.com/lib/pq"
)

const (
	pgUniqueViolationCode = "23505"
)

func IsUniqueConstraintViolation(err error) (string, bool) {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		if string(pqErr.Code) == pgUniqueViolationCode {
			return pqErr.Constraint, true
		}
	}
	return "", false
}
