package stream

// groupView is the bucketed L2 book for one (market, grouping), shared by every client subscribed at
// that grouping (docs/event-log.md §5). Native prices are aggregated into buckets of `group` price
// units — bids floored, asks ceiled — so the displayed spread is never tighter than the real one.
// It is maintained incrementally from canonical deltas and rebuilt from the canonical cache on
// resync; owned solely by the Hub goroutine, so it needs no lock. With group == price_quantum (and
// every price already a multiple of it) bucketing is the identity and native L2 passes through.
type groupView struct {
	group   uint64
	bids    map[uint64]uint64 // bucket -> aggregate qty
	asks    map[uint64]uint64
	clients map[*marketclient]struct{}
}

func newGroupView(group uint64, c *bookCache) *groupView {
	v := &groupView{group: group, clients: map[*marketclient]struct{}{}}
	v.rebuild(c)
	return v
}

// rebuild recomputes the bucketed book from the canonical cache (at creation and on resync). Clients
// are left untouched.
func (v *groupView) rebuild(c *bookCache) {
	v.bids = make(map[uint64]uint64, len(c.bids))
	v.asks = make(map[uint64]uint64, len(c.asks))
	for p, q := range c.bids {
		v.bids[bucketPrice("buy", p, v.group)] += q
	}
	for p, q := range c.asks {
		v.asks[bucketPrice("sell", p, v.group)] += q
	}
}

// applyDelta moves a bucket by a signed canonical change and returns the bucket's new aggregate
// quantity. A bucket that reaches zero is removed and reported as quantity 0 (level gone). The
// invariant (bucket == sum of its canonical levels) holds because every canonical delta maps to
// exactly one bucket and is applied exactly once.
func (v *groupView) applyDelta(side string, bucket uint64, delta int64) uint64 {
	m := v.sideMap(side)
	next := int64(m[bucket]) + delta
	if next <= 0 {
		delete(m, bucket)
		return 0
	}
	m[bucket] = uint64(next)
	return uint64(next)
}

func (v *groupView) sideMap(side string) map[uint64]uint64 {
	if side == "buy" {
		return v.bids
	}
	return v.asks
}

func (v *groupView) snapshotView() (bids, asks []bookLevel) {
	return sortedLevels(v.bids, true), sortedLevels(v.asks, false)
}

// bucketPrice maps a native price to its bucket boundary: bids floor, asks ceil to a multiple of
// group, so the bucketed spread only ever widens. price_quantum is the floor resolution, so group is
// always a multiple of it and you can only aggregate up.
func bucketPrice(side string, price, group uint64) uint64 {
	if group <= 1 {
		return price
	}
	rem := price % group
	if rem == 0 {
		return price
	}
	if side == "buy" {
		return price - rem // floor
	}
	return price - rem + group // ceil
}
