package utils

import "strings"

// NilIfBlank trims s and returns nil if nothing is left, else a pointer to the
// trimmed string. Useful for optional text fields that should be stored as SQL
// NULL rather than an empty string.
func NilIfBlank(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
