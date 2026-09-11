package utils

import "strings"

// DefaultIfBlank returns fallback when s is empty or only whitespace. For rendering a value that may
// legitimately be absent (a placeholder in a table, say) without an if at every call site.
func DefaultIfBlank(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// FormatUint64PtrOr renders an optional number, substituting fallback for nil. FormatUint64Ptr keeps
// the absence as a nil *string for JSON; this one is for display, where something has to be printed.
func FormatUint64PtrOr(v *uint64, fallback string) string {
	if v == nil {
		return fallback
	}
	return FormatUint64(*v)
}

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
