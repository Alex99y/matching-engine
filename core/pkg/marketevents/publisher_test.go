// NewPublisher, Run and Close all drive a real AMQP channel and are left to integration testing,
// matching the convention in common/pkg/rabbitmq. What is covered here is the back-pressure policy —
// the part that is pure channel arithmetic and the part that must never block the matcher.
package marketevents

import (
	"sync"
	"testing"
	"time"
)

// newTestPublisher builds a Publisher around a buffer of the given size, skipping the exchange that
// NewPublisher would open. Enqueue only ever touches ch and dropped.
func newTestPublisher(buffer int) *Publisher {
	return &Publisher{
		ch:   make(chan outbound, buffer),
		done: make(chan struct{}),
	}
}

func TestEnqueueAcceptsUntilTheBufferIsFull(t *testing.T) {
	p := newTestPublisher(2)

	for i := range 2 {
		if !p.Enqueue("market.ETH-USDT.book", "id", []byte("{}")) {
			t.Fatalf("event %d rejected while the buffer had room", i)
		}
	}
	if p.Dropped() != 0 {
		t.Fatalf("dropped = %d before the buffer filled, want 0", p.Dropped())
	}
}

// The matcher calls Enqueue on its own goroutine. Blocking here would stall matching on a slow
// broker, so a full buffer drops the event and says so rather than waiting for room.
func TestEnqueueDropsRatherThanBlocking(t *testing.T) {
	p := newTestPublisher(1)

	if !p.Enqueue("market.ETH-USDT.book", "id-1", []byte("{}")) {
		t.Fatal("first event rejected by an empty buffer")
	}

	// This one has nowhere to go: nothing is draining ch. It must return, not block.
	done := make(chan bool, 1)
	go func() { done <- p.Enqueue("market.ETH-USDT.book", "id-2", []byte("{}")) }()

	select {
	case accepted := <-done:
		if accepted {
			t.Fatal("a full buffer accepted an event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue blocked on a full buffer")
	}

	if p.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", p.Dropped())
	}
}

// Dropped is what the sequence gap is reconciled against, so every refused event has to be counted
// exactly once.
func TestEveryDropIsCounted(t *testing.T) {
	p := newTestPublisher(1)

	for range 5 {
		p.Enqueue("market.ETH-USDT.book", "id", []byte("{}"))
	}

	// One landed in the buffer; the other four had nowhere to go.
	if p.Dropped() != 4 {
		t.Fatalf("dropped = %d, want 4", p.Dropped())
	}
}

// One buffer is shared by every market's matcher goroutine, so the counter has to be safe under
// concurrent producers. Run with -race to mean anything.
func TestConcurrentProducersAccountForEveryEvent(t *testing.T) {
	const (
		producers = 8
		perGo     = 100
		buffer    = 16
	)
	p := newTestPublisher(buffer)

	var wg sync.WaitGroup
	var accepted [producers]int
	for i := range producers {
		wg.Go(func() {
			for range perGo {
				if p.Enqueue("market.ETH-USDT.book", "id", []byte("{}")) {
					accepted[i]++
				}
			}
		})
	}
	wg.Wait()

	total := 0
	for _, n := range accepted {
		total += n
	}
	if total > buffer {
		t.Fatalf("accepted %d events into a buffer of %d", total, buffer)
	}
	if got := uint64(producers*perGo - total); p.Dropped() != got {
		t.Fatalf("dropped = %d, want %d (accepted %d of %d)", p.Dropped(), got, total, producers*perGo)
	}
}

// The event reaches the publisher goroutine intact — a swapped routing key would fan a market's
// events out to the wrong subscribers.
func TestEnqueuePreservesTheEvent(t *testing.T) {
	p := newTestPublisher(1)

	body := []byte(`{"seq":1}`)
	if !p.Enqueue("market.ETH-USDT.trade", "msg-1", body) {
		t.Fatal("event rejected")
	}

	got := <-p.ch
	if got.routingKey != "market.ETH-USDT.trade" || got.messageId != "msg-1" {
		t.Fatalf("event = %+v", got)
	}
	if string(got.body) != string(body) {
		t.Fatalf("body = %s, want %s", got.body, body)
	}
}
