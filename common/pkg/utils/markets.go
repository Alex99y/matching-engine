package utils

import (
	"errors"
	"strings"
)

const MARKET_SEPARATOR = "-"

var ErrInvalidMarketRef = errors.New("invalid market reference, expected BASE/QUOTE")

func SplitMarketRef(marketRef string) (baseSymbol, quoteSymbol string, err error) {
	parts := strings.SplitN(marketRef, MARKET_SEPARATOR, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ErrInvalidMarketRef
	}
	return parts[0], parts[1], nil
}

func MergeMarketRef(baseSymbol, quoteSymbol string) string {
	return baseSymbol + MARKET_SEPARATOR + quoteSymbol
}
