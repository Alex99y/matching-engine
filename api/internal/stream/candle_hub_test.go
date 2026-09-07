package stream

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type stubSeeder struct {
	calls  atomic.Int32
	block  chan struct{} // when non-nil, GetCurrentCandle waits on it
	candle *repository.Candle
	err    error
}

func (s *stubSeeder) GetCurrentCandle(ctx context.Context, marketID int, bucketStart time.Time) (*repository.Candle, error) {
	s.calls.Add(1)
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.candle, s.err
}

func newTestCandleHub(seeder CandleSeeder) *CandleHub {
	return &CandleHub{
		logger:     logger.NewLogger(logger.Error),
		db:         seeder,
		marketIDs:  map[string]int{testMarket: 1},
		clients:    map[string]map[int64]map[*candleClient]struct{}{testMarket: {}},
		buckets:    map[string]map[int64]int64{testMarket: {}},
		events:     make(chan event, 8),
		register:   make(chan *candleClient, 4),
		unregister: make(chan *candleClient, 4),
		done:       make(chan struct{}),
	}
}

// The hub is one goroutine: any DB call inside handleRegister stalls every other client's
// stream for its duration. Registration must therefore touch the seeder not at all.
func TestHandleRegisterDoesNotTouchTheDatabase(t *testing.T) {
	seeder := &stubSeeder{}
	h := newTestCandleHub(seeder)

	snapshot, _ := h.Seed(context.Background(), testMarket, 60)
	if seeder.calls.Load() != 1 {
		t.Fatalf("Seed made %d db calls, want exactly 1", seeder.calls.Load())
	}

	cl := &candleClient{market: testMarket, interval: 60, ch: make(chan []byte, 4), snapshot: snapshot}
	h.handleRegister(cl)

	if got := seeder.calls.Load(); got != 1 {
		t.Fatalf("handleRegister made a db call (total %d) — the seed is back on the hub goroutine", got)
	}
	select {
	case frame := <-cl.ch:
		if ty := frameType(t, frame); ty != "candle.snapshot" {
			t.Fatalf("first frame is %q, want candle.snapshot", ty)
		}
	default:
		t.Fatal("registration delivered no opening snapshot")
	}
}

// A slow seed must not stop the hub from dispatching trades. This is the regression the whole
// change exists for: previously the loop sat inside GetCurrentCandle and every connected
// client went silent — no candle frames, no keepalives — until it returned.
func TestSlowSeedDoesNotStallTheHubLoop(t *testing.T) {
	seeder := &stubSeeder{block: make(chan struct{})}
	h := newTestCandleHub(seeder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.loop(ctx)

	existing := &candleClient{market: testMarket, interval: 60, ch: make(chan []byte, 8), snapshot: candleSnapshotFrame(60, 0, nil)}
	h.connect(existing)
	if _, err := recvWithin(existing.ch); err != nil {
		t.Fatalf("existing client never got its snapshot: %v", err)
	}

	// A second client seeds against a seeder that never returns. Seed runs on this goroutine,
	// so drive it in the background exactly as a real request would.
	seedDone := make(chan struct{})
	go func() {
		defer close(seedDone)
		h.Seed(context.Background(), testMarket, 60)
	}()

	// While that seed is stuck, the hub must still deliver a trade to the already-connected
	// client. Before the fix this timed out.
	h.events <- publicEvent(t, marketdata.EventTrade, "e1", 1,
		marketdata.Trade{Price: 100, Quantity: 5, TakerSide: "buy"})

	frame, err := recvWithin(existing.ch)
	if err != nil {
		t.Fatalf("hub stalled while a seed was in flight: %v", err)
	}
	if ty := frameType(t, frame); ty != "candle.trade" {
		t.Fatalf("got frame %q, want candle.trade", ty)
	}

	close(seeder.block)
	<-seedDone
}

var errTimeout = errors.New("timed out waiting for a frame")

// recvWithin blocks, unlike hub_test.go's recv — these tests wait on the hub goroutine.
func recvWithin(ch chan []byte) ([]byte, error) {
	select {
	case f := <-ch:
		return f, nil
	case <-time.After(2 * time.Second):
		return nil, errTimeout
	}
}
