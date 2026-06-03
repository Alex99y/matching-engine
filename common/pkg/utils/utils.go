package utils

import "strconv"

func StringToInt(value string) (int, error) {
	number, err := strconv.Atoi(value)
	return number, err
}
