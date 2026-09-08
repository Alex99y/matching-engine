package orderprocessors

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/core/pkg/deadletter"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
)

// deliveryFor builds a delivery around an arbitrary envelope, recording ack/nack. A nil event
// stands for a message the consumer could not parse at all.
func deliveryFor(rec *ackRecorder, event *oeq.OrderEvent, raw []byte) *oeq.OrderDelivery {
	return oeq.NewOrderDelivery(event, raw, "test-id",
		func() error { rec.mu.Lock(); rec.acks++; rec.mu.Unlock(); return nil },
		func() error { rec.mu.Lock(); rec.nacks++; rec.mu.Unlock(); return nil },
	)
}

func newTestProcessor(dlq deadLetterer) *OrderProcessor {
	return NewOrderProcessor(logger.NewLogger(logger.Error), testMarket(),
		&fakeQueue{}, &fakeRepo{}, nil, nil, dlq, "")
}

// Every command that can never be processed is parked and acked — never dropped silently and never
// requeued. Acking is what stops the broker redelivering a message that is guaranteed to fail again.
func TestHandleDeliveryParksUnprocessableCommands(t *testing.T) {
	unknownType, err := json.Marshal(oeq.OrderEvent{Type: "wat", Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		event         *oeq.OrderEvent
		raw           []byte
		want          deadletter.Reason
		wantEventType string // empty when the envelope itself never parsed
	}{
		{
			name: "envelope did not parse",
			raw:  []byte("not json at all"),
			want: deadletter.ReasonMalformed,
		},
		{
			name:          "payload did not decode",
			event:         &oeq.OrderEvent{Type: oeq.EventTypeOpenOrder, Payload: []byte(`{"price":"nope"}`)},
			raw:           []byte(`{"type":"open_order","payload":{"price":"nope"}}`),
			want:          deadletter.ReasonMalformed,
			wantEventType: string(oeq.EventTypeOpenOrder),
		},
		{
			name:          "unknown event type",
			event:         &oeq.OrderEvent{Type: "wat", Payload: []byte(`{}`)},
			raw:           unknownType,
			want:          deadletter.ReasonUnknownType,
			wantEventType: "wat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &ackRecorder{}
			dlq := &fakeDeadLetterer{}
			p := newTestProcessor(dlq)

			p.handleDelivery(context.Background(), deliveryFor(rec, tt.event, tt.raw))

			if got := dlq.reasons(); len(got) != 1 || got[0] != tt.want {
				t.Fatalf("parked reasons=%v want [%s]", got, tt.want)
			}
			if len(dlq.parked[0].Payload) == 0 {
				t.Fatal("parked envelope must carry the original payload")
			}
			if got := dlq.parked[0].EventType; got != tt.wantEventType {
				t.Fatalf("parked event_type=%q want %q", got, tt.wantEventType)
			}
			a, n := rec.counts()
			if a != 1 || n != 0 {
				t.Fatalf("acks=%d nacks=%d want 1/0", a, n)
			}
			if len(p.ordersChannel) != 0 {
				t.Fatal("an unprocessable command must never reach the matcher")
			}
		})
	}
}

// An order that is well-formed but violates the market's constraints is parked as "invalid" rather
// than acked and dropped, so a client-visible rejection is inspectable afterwards.
func TestHandleDeliveryParksInvalidOrder(t *testing.T) {
	open := limitBuy()
	open.Quantity = 0 // fails ValidateOrderEvent
	env, err := oeq.NewOpenOrderEvent(open)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.ToBytes()
	if err != nil {
		t.Fatal(err)
	}

	rec := &ackRecorder{}
	dlq := &fakeDeadLetterer{}
	p := newTestProcessor(dlq)

	p.handleDelivery(context.Background(), deliveryFor(rec, env, raw))

	if got := dlq.reasons(); len(got) != 1 || got[0] != deadletter.ReasonInvalid {
		t.Fatalf("parked reasons=%v want [%s]", got, deadletter.ReasonInvalid)
	}
	// The owner is known here, so the envelope must identify the order it belongs to.
	if dlq.parked[0].OrderID != open.OrderID.String() {
		t.Fatalf("parked order=%q want %q", dlq.parked[0].OrderID, open.OrderID)
	}
	if a, n := rec.counts(); a != 1 || n != 0 {
		t.Fatalf("acks=%d nacks=%d want 1/0", a, n)
	}
}

// A valid command reaches the matcher and is left unacknowledged — ack-after-commit owns it now.
func TestHandleDeliveryForwardsValidOrder(t *testing.T) {
	rec := &ackRecorder{}
	dlq := &fakeDeadLetterer{}
	p := newTestProcessor(dlq)

	p.handleDelivery(context.Background(), rec.delivery(limitBuy()))

	if dlq.count() != 0 {
		t.Fatalf("parked %d valid orders", dlq.count())
	}
	if a, n := rec.counts(); a != 0 || n != 0 {
		t.Fatalf("acks=%d nacks=%d want 0/0 — the matcher acks after commit", a, n)
	}
	if len(p.ordersChannel) != 1 {
		t.Fatalf("matcher received %d events, want 1", len(p.ordersChannel))
	}
}

// A publish failure must not requeue: the message is dropped so a sick broker cannot wedge a market.
func TestHandleDeliveryAcksWhenParkingFails(t *testing.T) {
	rec := &ackRecorder{}
	dlq := &fakeDeadLetterer{failWith: context.DeadlineExceeded}
	p := newTestProcessor(dlq)

	p.handleDelivery(context.Background(), deliveryFor(rec, nil, []byte("garbage")))

	if a, n := rec.counts(); a != 1 || n != 0 {
		t.Fatalf("acks=%d nacks=%d want 1/0", a, n)
	}
}
