package correlate_test

import (
	"testing"
	"time"

	"github.com/alex99y/matching-engine/loadtest/internal/correlate"
	"github.com/alex99y/matching-engine/loadtest/internal/stream"
	"github.com/google/uuid"
)

func isFilled(e stream.OrderEvent) bool { return e.Status == stream.StatusFilled }

func TestTrackerResolvesAMatchingSample(t *testing.T) {
	tr := correlate.NewTracker(isFilled)
	id := uuid.New()
	sentAt := time.Now()
	tr.Track(id, sentAt)

	receivedAt := sentAt.Add(50 * time.Millisecond)
	tr.Feed(stream.OrderEvent{OrderID: id, Status: stream.StatusFilled, ReceivedAt: receivedAt})

	samples := tr.Samples()
	if len(samples) != 1 {
		t.Fatalf("Samples() len = %d, want 1", len(samples))
	}
	if samples[0].Latency != 50*time.Millisecond {
		t.Errorf("Latency = %v, want 50ms", samples[0].Latency)
	}
	if tr.PendingCount() != 0 {
		t.Errorf("PendingCount() = %d, want 0", tr.PendingCount())
	}
}

func TestTrackerIgnoresEventsForUntrackedOrders(t *testing.T) {
	tr := correlate.NewTracker(isFilled)
	tr.Feed(stream.OrderEvent{OrderID: uuid.New(), Status: stream.StatusFilled, ReceivedAt: time.Now()})

	if len(tr.Samples()) != 0 {
		t.Errorf("expected no samples for an order never tracked")
	}
}

func TestTrackerNonMatchingIntermediateEventLeavesOrderPending(t *testing.T) {
	tr := correlate.NewTracker(isFilled)
	id := uuid.New()
	tr.Track(id, time.Now())

	// "open" is not terminal and doesn't match — the order must stay pending, awaiting a fill.
	tr.Feed(stream.OrderEvent{OrderID: id, Status: stream.StatusOpen, ReceivedAt: time.Now()})

	if tr.PendingCount() != 1 {
		t.Errorf("PendingCount() = %d, want 1 (still awaiting a fill)", tr.PendingCount())
	}
	if len(tr.Samples()) != 0 {
		t.Errorf("expected no samples yet")
	}
}

func TestTrackerTerminalNonMatchIsDeadNotTimeout(t *testing.T) {
	tr := correlate.NewTracker(isFilled)
	id := uuid.New()
	tr.Track(id, time.Now())

	// "cancelled" is terminal and will never satisfy isFilled — must resolve immediately as Dead,
	// not linger in pending until a Sweep times it out.
	tr.Feed(stream.OrderEvent{OrderID: id, Status: stream.StatusCancelled, ReceivedAt: time.Now()})

	if tr.PendingCount() != 0 {
		t.Errorf("PendingCount() = %d, want 0", tr.PendingCount())
	}
	dead := tr.Dead()
	if len(dead) != 1 || dead[0].OrderID != id {
		t.Fatalf("Dead() = %+v, want one entry for %s", dead, id)
	}
	if len(tr.Samples()) != 0 {
		t.Errorf("expected no samples")
	}
}

func TestTrackerSweepTimesOutStaleOrders(t *testing.T) {
	tr := correlate.NewTracker(isFilled)
	id := uuid.New()
	sentAt := time.Now().Add(-time.Minute)
	tr.Track(id, sentAt)

	tr.Sweep(time.Now())

	if tr.PendingCount() != 0 {
		t.Errorf("PendingCount() = %d, want 0 after sweep", tr.PendingCount())
	}
	timeouts := tr.Timeouts()
	if len(timeouts) != 1 || timeouts[0].OrderID != id {
		t.Fatalf("Timeouts() = %+v, want one entry for %s", timeouts, id)
	}
}

func TestTrackerSweepDoesNotTimeOutOrdersBeforeCutoff(t *testing.T) {
	tr := correlate.NewTracker(isFilled)
	id := uuid.New()
	tr.Track(id, time.Now())

	tr.Sweep(time.Now().Add(-time.Hour))

	if tr.PendingCount() != 1 {
		t.Errorf("PendingCount() = %d, want 1 (not old enough to sweep)", tr.PendingCount())
	}
	if len(tr.Timeouts()) != 0 {
		t.Errorf("expected no timeouts")
	}
}

func TestTrackerResolvedOrderIgnoresLaterEvents(t *testing.T) {
	tr := correlate.NewTracker(isFilled)
	id := uuid.New()
	tr.Track(id, time.Now())
	tr.Feed(stream.OrderEvent{OrderID: id, Status: stream.StatusFilled, ReceivedAt: time.Now()})

	// A stray duplicate/late event for the same (already resolved) order must not add a second
	// sample or reappear in Dead/Timeouts.
	tr.Feed(stream.OrderEvent{OrderID: id, Status: stream.StatusCancelled, ReceivedAt: time.Now()})

	if len(tr.Samples()) != 1 {
		t.Errorf("Samples() len = %d, want 1", len(tr.Samples()))
	}
	if len(tr.Dead()) != 0 {
		t.Errorf("expected no dead entries")
	}
}
