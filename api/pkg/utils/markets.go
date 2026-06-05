package utils

import (
	"errors"
	"strings"
)

var ErrInvalidMarketRef = errors.New("invalid market reference, expected BASE/QUOTE")

func SplitMarketRef(marketRef string) (baseSymbol, quoteSymbol string, err error) {
	parts := strings.SplitN(marketRef, "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ErrInvalidMarketRef
	}
	return parts[0], parts[1], nil
}
