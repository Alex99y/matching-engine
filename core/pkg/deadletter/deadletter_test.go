// deadletter_test.go covers the broker-independent pieces: queue naming, the classification both
// the matcher and the consumer rely on, the poison error hand-off, and payload encoding.
// DeclareTopology, NewConsumer and Run drive a real AMQP channel and are left to the live check.
package deadletter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/google/uuid"
)

func TestQueueNameIsDerivedFromMarketRef(t *testing.T) {
	got := QueueName("ETH-USDT")
	if got != "me.dlq.ETH-USDT" {
		t.Fatalf("QueueName = %q, want %q", got, "me.dlq.ETH-USDT")
	}
	if !strings.HasPrefix(got, queuePrefix) {
		t.Fatalf("QueueName %q must be namespaced under %q", got, queuePrefix)
	}
}

func openOrderBytes(t *testing.T, open *oeq.OpenOrderEvent) []byte {
	t.Helper()
	env, err := oeq.NewOpenOrderEvent(open)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func validOpen() *oeq.OpenOrderEvent {
	return &oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 10,
	}
}

// The consumer recomputes a dead letter's reason from its payload, so this table is the contract
// that the row an operator reads says the same thing the matcher decided.
func TestClassifyRaw(t *testing.T) {
	none := oeq.MarketConstraints{}

	invalid := validOpen()
	invalid.Price = 0

	cancelEnv, err := oeq.NewCancelOrderEvent(&oeq.CancelOrderEvent{OrderID: uuid.New(), MarketRef: "ETH-USDT"})
	if err != nil {
		t.Fatal(err)
	}
	cancelRaw, err := cancelEnv.ToBytes()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		raw       []byte
		reason    Reason
		eventType string
		hasOrder  bool
	}{
		{"garbage bytes", []byte("not json"), ReasonMalformed, "", false},
		{"envelope with unparseable payload", []byte(`{"type":"open_order","payload":"nope"}`), ReasonMalformed, "open_order", false},
		{"unknown type", []byte(`{"type":"amend_order","payload":{}}`), ReasonUnknownType, "amend_order", false},
		{"invalid order", openOrderBytes(t, invalid), ReasonInvalid, "open_order", true},
		{"processable order", openOrderBytes(t, validOpen()), "", "open_order", true},
		{"processable cancel", cancelRaw, "", "cancel_order", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ClassifyRaw(tt.raw, none)
			if v.Reason != tt.reason {
				t.Fatalf("reason = %q (%s), want %q", v.Reason, v.Error, tt.reason)
			}
			if v.Dead() != (tt.reason != "") {
				t.Fatalf("Dead() = %v for reason %q", v.Dead(), v.Reason)
			}
			if v.EventType != tt.eventType {
				t.Fatalf("event type = %q, want %q", v.EventType, tt.eventType)
			}
			if (v.OrderID != uuid.Nil) != tt.hasOrder {
				t.Fatalf("order id = %s, want present=%v", v.OrderID, tt.hasOrder)
			}
			if v.Dead() && v.Error == "" {
				t.Fatal("a dead verdict must say why")
			}
		})
	}
}

// An invalid order still identifies itself, so the matcher can tell its owner it was refused.
func TestClassifyKeepsTheOrderOnAnInvalidVerdict(t *testing.T) {
	invalid := validOpen()
	invalid.Quantity = 0
	v := ClassifyRaw(openOrderBytes(t, invalid), oeq.MarketConstraints{})
	if v.Reason != ReasonInvalid || v.Open == nil || v.Open.UserID != invalid.UserID {
		t.Fatalf("verdict = %+v, want invalid with the decoded order attached", v)
	}
}

func TestPoisonErrorsHandOffOnceAndExpire(t *testing.T) {
	now := time.Unix(1_757_000_000, 0)
	p := NewPoisonErrors()
	p.now = func() time.Time { return now }

	p.Record("order-1", "open_order", "pq: constraint")
	p.Record("order-1", "cancel_order", "other") // the same id, a different command

	if got, ok := p.Take("order-1", "open_order"); !ok || got != "pq: constraint" {
		t.Fatalf("Take = (%q, %v), want the recorded text", got, ok)
	}
	if _, ok := p.Take("order-1", "open_order"); ok {
		t.Fatal("a text must be handed off once")
	}
	if got, ok := p.Take("order-1", "cancel_order"); !ok || got != "other" {
		t.Fatalf("the cancel's text was lost: (%q, %v)", got, ok)
	}

	p.Record("stale", "open_order", "never consumed")
	now = now.Add(poisonErrorTTL + time.Second)
	p.Record("fresh", "open_order", "x")
	if _, ok := p.Take("stale", "open_order"); ok {
		t.Fatal("an entry older than the TTL must be evicted on the next record")
	}
}

// A malformed command must be kept verbatim even though the column is JSONB.
func TestJSONPayloadStoresAnyBytes(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{`{"type":"open_order"}`, `{"type":"open_order"}`},
		{`not json`, `"not json"`},
		{``, `null`},
	} {
		got, err := jsonPayload([]byte(tt.in))
		if err != nil {
			t.Fatalf("jsonPayload(%q): %v", tt.in, err)
		}
		if string(got) != tt.want || !json.Valid(got) {
			t.Fatalf("jsonPayload(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}
