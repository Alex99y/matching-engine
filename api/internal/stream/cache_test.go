package stream

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/marketdata"
)

func snap(epoch string, seq uint64, bids, asks []marketdata.BookLevel) marketdata.Snapshot {
	return marketdata.Snapshot{Epoch: epoch, Seq: seq, Market: "BTC-USDT", Bids: bids, Asks: asks}
}

func bookDelta(side string, price, qty uint64) marketdata.Book {
	return marketdata.Book{Side: side, Price: price, Quantity: qty}
}

// A delta arriving before the first snapshot is dropped: the cache is not yet synced.
func TestCacheIgnoresDeltaBeforeSnapshot(t *testing.T) {
	c := newBookCache()
	if c.applyDelta("e1", 1, bookDelta("buy", 100, 5)) {
		t.Fatal("delta applied before any snapshot")
	}
}

// After a snapshot, strictly in-order deltas apply; a zero quantity removes the level.
func TestCacheAppliesInOrderDeltas(t *testing.T) {
	c := newBookCache()
	c.applySnapshot(snap("e1", 10,
		[]marketdata.BookLevel{{Price: 100, Quantity: 5}},
		[]marketdata.BookLevel{{Price: 101, Quantity: 3}},
	))

	if !c.applyDelta("e1", 11, bookDelta("buy", 99, 7)) {
		t.Fatal("seq 11 (lastSeq+1) should apply")
	}
	if c.bids[99] != 7 {
		t.Fatalf("bid 99 = %d, want 7", c.bids[99])
	}
	if !c.applyDelta("e1", 12, bookDelta("buy", 100, 0)) {
		t.Fatal("seq 12 should apply")
	}
	if _, ok := c.bids[100]; ok {
		t.Fatal("zero quantity should remove the level")
	}
	if c.lastSeq != 12 {
		t.Fatalf("lastSeq = %d, want 12", c.lastSeq)
	}
}

// A sequence gap unsyncs the cache and drops subsequent deltas until the next snapshot re-syncs.
func TestCacheGapUnsyncsUntilSnapshot(t *testing.T) {
	c := newBookCache()
	c.applySnapshot(snap("e1", 10, nil, nil))

	if c.applyDelta("e1", 12, bookDelta("buy", 100, 5)) { // skips 11
		t.Fatal("gapped delta should not apply")
	}
	if c.synced {
		t.Fatal("cache should be unsynced after a gap")
	}
	if c.applyDelta("e1", 13, bookDelta("buy", 100, 5)) {
		t.Fatal("deltas must stay dropped while unsynced")
	}

	c.applySnapshot(snap("e1", 20, []marketdata.BookLevel{{Price: 100, Quantity: 9}}, nil))
	if !c.synced || c.lastSeq != 20 {
		t.Fatalf("snapshot should re-sync: synced=%v lastSeq=%d", c.synced, c.lastSeq)
	}
	if !c.applyDelta("e1", 21, bookDelta("buy", 100, 4)) {
		t.Fatal("delta after re-sync should apply")
	}
}

// An epoch change (core restarted) unsyncs the cache even on an otherwise in-order seq.
func TestCacheEpochChangeUnsyncs(t *testing.T) {
	c := newBookCache()
	c.applySnapshot(snap("e1", 10, nil, nil))
	if c.applyDelta("e2", 11, bookDelta("buy", 100, 5)) {
		t.Fatal("delta from a new epoch should not apply")
	}
	if c.synced {
		t.Fatal("epoch change should unsync")
	}
}

// A heartbeat running ahead of the last applied seq reveals a silent gap and unsyncs the cache.
func TestCacheHeartbeatDetectsGap(t *testing.T) {
	c := newBookCache()
	c.applySnapshot(snap("e1", 10, nil, nil))

	c.checkHeartbeat("e1", 10) // caught up
	if !c.synced {
		t.Fatal("matching heartbeat must not unsync")
	}
	c.checkHeartbeat("e1", 15) // we missed 11..15
	if c.synced {
		t.Fatal("heartbeat ahead of lastSeq must unsync")
	}
}
