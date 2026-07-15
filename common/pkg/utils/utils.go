package utils

import "strconv"

func StringToInt(value string) (int, error) {
	number, err := strconv.Atoi(value)
	return number, err
}

func FormatUint64(v uint64) string { return strconv.FormatUint(v, 10) }
