// deadletter_test.go covers the broker-independent pieces: queue naming and the envelope wire
// format. NewPublisher, DeclareMarket and Publish all drive a real AMQP channel and are left to
// integration testing, matching the convention in common/pkg/rabbitmq.
package deadletter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
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

// An operator replays a parked command by hand, so the envelope must survive the round trip with
// the original payload byte-for-byte and the diagnosis intact.
func TestEnvelopeRoundTrip(t *testing.T) {
	payload := []byte(`{"type":"open_order","payload":{"price":100}}`)
	in := Envelope{
		OrderID:   "0199c0de-0000-7000-8000-000000000001",
		MarketRef: "ETH-USDT",
		EventType: "open_order",
		Reason:    ReasonPoison,
		Error:     `pq: duplicate key value violates "orders_client_order_id_user_id_uk"`,
		Failures:  10,
		DeadAt:    time.Unix(1757000000, 0).UTC(),
		Payload:   payload,
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Envelope
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}

	if string(out.Payload) != string(payload) {
		t.Fatalf("payload = %s, want %s", out.Payload, payload)
	}
	if out.Reason != in.Reason || out.Error != in.Error || out.Failures != in.Failures {
		t.Fatalf("diagnosis lost: %+v", out)
	}
	if !out.DeadAt.Equal(in.DeadAt) {
		t.Fatalf("dead_at = %v, want %v", out.DeadAt, in.DeadAt)
	}
	if out.OrderID != in.OrderID || out.MarketRef != in.MarketRef || out.EventType != in.EventType {
		t.Fatalf("identity lost: %+v", out)
	}
}

// The malformed reason exists precisely for messages that are not valid JSON, so the envelope must
// survive one. Payload is a json.RawMessage — emitted verbatim — so an unnormalised one would fail
// the whole marshal and the messages most in need of parking would be the only ones that could not
// be parked.
func TestNormalisePayloadKeepsUnparseableMessagesMarshalable(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"valid JSON is preserved verbatim", []byte(`{"type":"open_order"}`), `{"type":"open_order"}`},
		{"garbage becomes a JSON string", []byte("this is not json at all"), `"this is not json at all"`},
		{"quotes are escaped", []byte(`he said "hi"`), `"he said \"hi\""`},
		{"empty stays empty", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &Envelope{MarketRef: "ETH-USDT", Reason: ReasonMalformed, Payload: tt.in}
			if err := normalisePayload(env); err != nil {
				t.Fatal(err)
			}
			if string(env.Payload) != tt.want {
				t.Fatalf("payload = %s, want %s", env.Payload, tt.want)
			}
			if _, err := json.Marshal(env); err != nil {
				t.Fatalf("envelope must always marshal: %v", err)
			}
		})
	}
}

// A quarantined expiry has no broker message and no order payload behind it; those fields must drop
// out of the wire format rather than appearing as empty noise an operator has to read past.
func TestEnvelopeOmitsEmptyOptionalFields(t *testing.T) {
	raw, err := json.Marshal(Envelope{MarketRef: "ETH-BTC", Reason: ReasonQuarantined})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"payload", "error", "failures", "event_type", "order_id"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("empty %q should be omitted, got %s", field, raw)
		}
	}
	for _, field := range []string{"market_ref", "reason", "dead_at"} {
		if !strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("%q must always be present, got %s", field, raw)
		}
	}
}
