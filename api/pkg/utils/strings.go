package utils

func NilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
