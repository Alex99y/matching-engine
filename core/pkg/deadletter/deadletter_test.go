// deadletter_test.go covers the broker-independent pieces: queue naming, the classification both
// the matcher and the consumer rely on, the poison error hand-off, and payload encoding.
// DeclareTopology, NewConsumer and Run drive a real AMQP channel and are left to the live check.
package deadletter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	mev1 "github.com/alex99y/matching-engine/common/pkg/pb/me/v1"
	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
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
	raw, err := oeq.NewOpenOrderEvent(open).ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func commandBytes(t *testing.T, command *mev1.OrderCommand) []byte {
	t.Helper()
	raw, err := proto.Marshal(command)
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

	cancelRaw, err := oeq.NewCancelOrderEvent(&oeq.CancelOrderEvent{OrderID: uuid.New(), MarketRef: "ETH-USDT"}).ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	badOrderID := commandBytes(t, &mev1.OrderCommand{
		Version: mev1.Version,
		Command: &mev1.OrderCommand_Open{Open: &mev1.OpenOrder{OrderId: []byte{1, 2, 3}}},
	})

	tests := []struct {
		name      string
		raw       []byte
		reason    Reason
		eventType string
		hasOrder  bool
	}{
		{"garbage bytes", []byte{0x80}, ReasonMalformed, "", false},
		{"another schema version", commandBytes(t, &mev1.OrderCommand{Version: 2}), ReasonMalformed, "", false},
		{"order id that is not a uuid", badOrderID, ReasonMalformed, "", false},
		{"unknown command", commandBytes(t, &mev1.OrderCommand{Version: mev1.Version}), ReasonUnknownType, "", false},
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

// The JSONB column holds the command rendered as JSON when it decodes, and the bytes themselves
// (base64) when it does not, so a malformed body is still kept whole.
func TestPayloadJSON(t *testing.T) {
	open := validOpen()
	rendered := payloadJSON(openOrderBytes(t, open))
	var decoded struct {
		Version uint32 `json:"version"`
		Open    struct {
			Quantity uint64 `json:"quantity,string"`
		} `json:"open"`
		Raw *string `json:"raw_base64"`
	}
	if err := json.Unmarshal(rendered, &decoded); err != nil {
		t.Fatalf("rendering is not JSON: %v\n%s", err, rendered)
	}
	if decoded.Version != mev1.Version || decoded.Open.Quantity != open.Quantity || decoded.Raw != nil {
		t.Fatalf("decodable body rendered as %s", rendered)
	}

	for name, raw := range map[string][]byte{
		"garbage":         {0x80},
		"another version": commandBytes(t, &mev1.OrderCommand{Version: 2}),
		"empty":           nil,
	} {
		t.Run(name, func(t *testing.T) {
			got := payloadJSON(raw)
			var fallback map[string][]byte
			if err := json.Unmarshal(got, &fallback); err != nil {
				t.Fatalf("payloadJSON = %s: %v", got, err)
			}
			if back, ok := fallback["raw_base64"]; !ok || len(fallback) != 1 || string(back) != string(raw) {
				t.Fatalf("payloadJSON = %s, want only raw_base64 of %v", got, raw)
			}
		})
	}
}
