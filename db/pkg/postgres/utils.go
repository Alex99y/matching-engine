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

// IsDataError reports whether err is a deterministic PostgreSQL data or integrity
// error — SQLSTATE class 22 ("data exception", e.g. value too long) or 23 ("integrity
// constraint violation", e.g. check/not-null/foreign-key). These fail identically on
// every retry, which marks the offending row as a poison message rather than a
// transient infrastructure failure (connection loss, deadlock, admin shutdown, ...).
func IsDataError(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		code := string(pqErr.Code)
		return len(code) >= 2 && (code[:2] == "22" || code[:2] == "23")
	}
	return false
}
