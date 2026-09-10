// Package admin is core's operator control plane: the per-market circuit breaker that halts and
// resumes trading. It is deliberately separate from the Prometheus listener — pausing a market is a
// privileged action, and the metrics port is scraped by monitoring that has no business halting a
// market.
package admin

import (
	"context"
	"errors"
	"sort"
)

var ErrMarketNotServed = errors.New("market not served by this core")

// MarketController is the subset of orderprocessors.OrderProcessor this service needs. Declared here
// (the consumer) per the layer-architecture rule; exported so main can build the map.
type MarketController interface {
	Pause()
	Resume()
	IsPaused() bool
}

type MarketStatus struct {
	Market string `json:"market"`
	Paused bool   `json:"paused"`
}

// Service halts and resumes the markets this core process serves. The set is fixed at startup from
// MARKET_LIST, so a ref that is absent is a genuine 404 rather than a market that merely has no
// orders — an operator halting the wrong core should be told, not silently ignored.
type Service struct {
	markets map[string]MarketController
}

func NewService(markets map[string]MarketController) *Service {
	if markets == nil {
		panic("markets cannot be nil")
	}
	return &Service{markets: markets}
}

func (s *Service) Pause(ctx context.Context, marketRef string) error {
	market, ok := s.markets[marketRef]
	if !ok {
		return ErrMarketNotServed
	}
	market.Pause()
	return nil
}

func (s *Service) Resume(ctx context.Context, marketRef string) error {
	market, ok := s.markets[marketRef]
	if !ok {
		return ErrMarketNotServed
	}
	market.Resume()
	return nil
}

func (s *Service) Status(ctx context.Context) []MarketStatus {
	out := make([]MarketStatus, 0, len(s.markets))
	for ref, market := range s.markets {
		out = append(out, MarketStatus{Market: ref, Paused: market.IsPaused()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Market < out[j].Market })
	return out
}
