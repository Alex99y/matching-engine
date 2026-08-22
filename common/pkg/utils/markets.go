package utils

import (
	"errors"
	"strconv"
	"strings"
)

const MARKET_SEPARATOR = "-"

var (
	ErrInvalidMarketRef  = errors.New("invalid market reference, expected BASE/QUOTE")
	ErrInvalidPriceGroup = errors.New("group must be a positive multiple of the market price quantum")
)

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

// ParsePriceGroup resolves a requested price-bucket size from a raw ?group query value. Empty
// means native resolution (the market's price_quantum). Otherwise it must be a positive multiple
// of price_quantum — price_quantum is the tick, so callers can only aggregate up from it. Shared
// by the SSE book stream and the REST depth snapshot, which bucket the same way.
func ParsePriceGroup(raw string, priceQuantum uint64) (uint64, error) {
	if priceQuantum == 0 {
		priceQuantum = 1 // defensive; markets always have a positive quantum
	}
	if raw == "" {
		return priceQuantum, nil
	}
	g, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || g == 0 || g%priceQuantum != 0 {
		return 0, ErrInvalidPriceGroup
	}
	return g, nil
}
