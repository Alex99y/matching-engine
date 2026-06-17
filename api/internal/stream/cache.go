// package stream is the api side of the live event-log (docs/event-log.md, Phase C): it
// subscribes to core's me.events topic exchange, keeps an in-memory L2 order-book cache per market
// in sync via the snapshot + sequenced-delta protocol, and fans events out to SSE clients. It never
// reads the database. C1 covers the public stream (book + trades); private per-user order events
// arrive in C2.
package stream

import (
	"sort"

	"github.com/alex99y/matching-engine/common/pkg/marketdata"
)

// bookCache is one market's L2 order book, kept in sync from the core event stream. It is owned
// exclusively by the Hub goroutine, so it needs no locking. The (epoch, seq) machinery lives here
// and core↔api only — clients never see it.
type bookCache struct {
	epoch   string
	lastSeq uint64
	synced  bool
	bids    map[uint64]uint64 // price -> aggregate quantity
	asks    map[uint64]uint64
}

func newBookCache() *bookCache {
	return &bookCache{bids: map[uint64]uint64{}, asks: map[uint64]uint64{}}
}

// applySnapshot resets the cache to the authoritative book at (epoch, seq) and marks it synced.
// This is the single recovery path: a sequence gap or an epoch change unsyncs the cache, and the
// next snapshot re-establishes it.
func (c *bookCache) applySnapshot(s marketdata.Snapshot) {
	c.epoch = s.Epoch
	c.lastSeq = s.Seq
	c.synced = true
	c.bids = make(map[uint64]uint64, len(s.Bids))
	c.asks = make(map[uint64]uint64, len(s.Asks))
	for _, l := range s.Bids {
		if l.Quantity > 0 {
			c.bids[l.Price] = l.Quantity
		}
	}
	for _, l := range s.Asks {
		if l.Quantity > 0 {
			c.asks[l.Price] = l.Quantity
		}
	}
}

// applyDelta applies one book delta iff it is the in-order successor of the last applied event,
// returning true so the caller forwards it to clients. An epoch change (core restarted) or a
// sequence gap (missed events) unsyncs the cache and drops the delta; while unsynced every delta is
// dropped until the next snapshot re-syncs. A zero quantity removes the level.
func (c *bookCache) applyDelta(epoch string, seq uint64, b marketdata.Book) bool {
	if !c.synced {
		return false
	}
	if epoch != c.epoch || seq != c.lastSeq+1 {
		c.synced = false // gap or restart; wait for the next snapshot
		return false
	}
	side := c.sideMap(b.Side)
	if b.Quantity == 0 {
		delete(side, b.Price)
	} else {
		side[b.Price] = b.Quantity
	}
	c.lastSeq = seq
	return true
}

// checkHeartbeat detects a gap while the book is otherwise idle: liveness carries the current seq,
// so if it runs ahead of what we applied (or the epoch changed) we missed deltas and must re-sync.
func (c *bookCache) checkHeartbeat(epoch string, seq uint64) {
	if c.synced && (epoch != c.epoch || seq != c.lastSeq) {
		c.synced = false
	}
}

func (c *bookCache) sideMap(side string) map[uint64]uint64 {
	if side == "buy" {
		return c.bids
	}
	return c.asks
}

type bookLevel struct {
	price uint64
	qty   uint64
}

// snapshotView returns the current book as sorted level slices (bids high→low, asks low→high) for
// the initial frame sent to a connecting client.
func (c *bookCache) snapshotView() (bids, asks []bookLevel) {
	bids = make([]bookLevel, 0, len(c.bids))
	for p, q := range c.bids {
		bids = append(bids, bookLevel{price: p, qty: q})
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i].price > bids[j].price })

	asks = make([]bookLevel, 0, len(c.asks))
	for p, q := range c.asks {
		asks = append(asks, bookLevel{price: p, qty: q})
	}
	sort.Slice(asks, func(i, j int) bool { return asks[i].price < asks[j].price })
	return bids, asks
}
