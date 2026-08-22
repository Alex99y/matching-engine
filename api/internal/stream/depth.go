package stream

import (
	"context"
	"errors"
)

// ErrHubClosed is returned by Depth when the Hub shuts down before the request could be served.
var ErrHubClosed = errors.New("stream hub is closed")

// DepthLevel is one price level of a one-shot depth snapshot (see Hub.Depth).
type DepthLevel struct {
	Price    uint64
	Quantity uint64
}

// MarketDepth is a one-shot snapshot of a market's book, bucketed to the requested grouping —
// the REST counterpart of the SSE snapshot frame (groupSnapshotFrame).
type MarketDepth struct {
	Market string
	Bids   []DepthLevel
	Asks   []DepthLevel
}

type depthRequest struct {
	market string
	group  uint64
	resp   chan depthResult
}

type depthResult struct {
	found bool
	bids  []bookLevel
	asks  []bookLevel
}

// Depth serves a one-shot REST snapshot (GET /markets/:market/depth) by round-tripping through the
// Hub's actor loop, the sole owner of book state — the same synchronization the register/unregister
// channels already use, so no lock is added to the hot event path. found is false when market isn't
// served by this Hub.
func (h *Hub) Depth(ctx context.Context, market string, group uint64) (*MarketDepth, bool, error) {
	if group == 0 {
		group = 1
	}
	resp := make(chan depthResult, 1)
	select {
	case h.depthReq <- depthRequest{market: market, group: group, resp: resp}:
	case <-h.done:
		return nil, false, ErrHubClosed
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	select {
	case res := <-resp:
		if !res.found {
			return nil, false, nil
		}
		return &MarketDepth{Market: market, Bids: depthLevelsOf(res.bids), Asks: depthLevelsOf(res.asks)}, true, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

// handleDepthRequest resolves one Depth() call on the Hub goroutine, aggregating the canonical
// cache the same way a persistent groupView would (bucketAggregate) without creating one.
func (h *Hub) handleDepthRequest(r depthRequest) {
	cache := h.caches[r.market]
	if cache == nil {
		r.resp <- depthResult{found: false}
		return
	}
	r.resp <- depthResult{
		found: true,
		bids:  sortedLevels(bucketAggregate(cache.bids, "buy", r.group), true),
		asks:  sortedLevels(bucketAggregate(cache.asks, "sell", r.group), false),
	}
}

func depthLevelsOf(levels []bookLevel) []DepthLevel {
	out := make([]DepthLevel, len(levels))
	for i, l := range levels {
		out[i] = DepthLevel{Price: l.price, Quantity: l.qty}
	}
	return out
}
