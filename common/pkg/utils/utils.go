package utils

import "strconv"

func StringToInt(value string) (int, error) {
	number, err := strconv.Atoi(value)
	return number, err
}

func FormatUint64(v uint64) string { return strconv.FormatUint(v, 10) }

func FormatUint64Ptr(v *uint64) *string {
	if v == nil {
		return nil
	}
	formatted := FormatUint64(*v)
	return &formatted
}
