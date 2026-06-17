package stream

import (
	"encoding/json"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/marketdata"
)

// Bids floor to their bucket, asks ceil — so the displayed spread never tightens. With group 1 (or a
// price already on a boundary) bucketing is the identity.
func TestBucketPrice(t *testing.T) {
	cases := []struct {
		side         string
		price, group uint64
		want         uint64
	}{
		{"buy", 103, 5, 100},  // bid floors down
		{"sell", 103, 5, 105}, // ask ceils up
		{"buy", 100, 5, 100},  // on a boundary: unchanged
		{"sell", 100, 5, 100}, // on a boundary: unchanged
		{"buy", 103, 1, 103},  // native
		{"sell", 103, 1, 103}, // native
	}
	for _, c := range cases {
		if got := bucketPrice(c.side, c.price, c.group); got != c.want {
			t.Errorf("bucketPrice(%s, %d, %d) = %d, want %d", c.side, c.price, c.group, got, c.want)
		}
	}
}

// A group view aggregates native levels into k×quantum buckets and sorts them (bids high→low,
// asks low→high).
func TestGroupViewAggregates(t *testing.T) {
	c := newBookCache()
	c.applySnapshot(marketdata.Snapshot{
		Epoch: "e1", Seq: 1, Market: testMarket,
		Bids: []marketdata.BookLevel{{Price: 103, Quantity: 4}, {Price: 102, Quantity: 1}, {Price: 98, Quantity: 10}},
		Asks: []marketdata.BookLevel{{Price: 106, Quantity: 2}, {Price: 109, Quantity: 3}},
	})
	v := newGroupView(5, c)

	bids, asks := v.snapshotView()
	// bids: 103,102 floor→100 (qty 5); 98 floor→95 (qty 10). Ordered high→low.
	if len(bids) != 2 || bids[0].price != 100 || bids[0].qty != 5 || bids[1].price != 95 || bids[1].qty != 10 {
		t.Fatalf("bids = %+v, want [{100 5} {95 10}]", bids)
	}
	// asks: 106 ceil→110 (qty 2); 109 ceil→110 (qty 3) → merged into 110 qty 5.
	if len(asks) != 1 || asks[0].price != 110 || asks[0].qty != 5 {
		t.Fatalf("asks = %+v, want [{110 5}]", asks)
	}
}

// A native delta is bucketed before it reaches a grouped client: the frame carries the bucket
// boundary and the bucket's new aggregate, not the raw price.
func TestHubBucketsDeltaForGroupedClient(t *testing.T) {
	h := newTestHub(&fakeSource{}, testMarket)
	h.handleEvent(publicEvent(t, marketdata.EventSnapshot, "e1", 1, marketdata.Snapshot{
		Epoch: "e1", Seq: 1, Market: testMarket,
		Bids: []marketdata.BookLevel{{Price: 102, Quantity: 1}},
	}))

	c := newMarketClient(testMarket, 5) // grouping of 5
	h.handleRegister(c)
	recv(t, c.ch) // drop initial bucketed snapshot

	// New native level at 103 → bucket 100; it now holds 102(1) + 103(4) = 5.
	h.handleEvent(publicEvent(t, marketdata.EventBook, "e1", 2, marketdata.Book{Side: "buy", Price: 103, Quantity: 4}))

	var msg bookMsg
	frame := recv(t, c.ch)
	payload := frame[len("data: ") : len(frame)-2]
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Type != "book" || msg.Side != "buy" || msg.Price != "100" || msg.Quantity != "5" {
		t.Fatalf("frame = %+v, want bucketed buy@100 qty 5", msg)
	}
}
