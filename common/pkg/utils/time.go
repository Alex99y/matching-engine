package utils

import (
	"strconv"
	"time"
)

func ParseUnixTimestamp(raw string) (time.Time, error) {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(v, 0).UTC(), nil
}
