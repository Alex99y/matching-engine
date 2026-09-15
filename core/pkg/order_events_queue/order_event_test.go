package order_events_queue

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The full journey of a command: the API builds it, serialises it onto the queue, and core parses
// and decodes it. Every field has to survive, and the three optional ones are pointers precisely so
// that "absent" and "zero" stay distinguishable — a dropped ExpiresAt turns a temporary order into a
// permanent one, and a dropped QuoteQty leaves a market buy with no budget to spend.
func TestOpenOrderSurvivesTheWireRoundTrip(t *testing.T) {
	in := &OpenOrderEvent{
		OrderID:       uuid.MustParse("0199c0de-0000-7000-8000-000000000001"),
		ClientOrderID: "my-order-1",
		MarketID:      7,
		UserID:        uuid.MustParse("0199c0de-0000-7000-8000-000000000002"),
		Side:          BuyOrder,
		Type:          LimitOrder,
		TimeInForce:   GoodTillCancel,
		Price:         79_200_000_000,
		Quantity:      500_000_000,
		QuoteQty:      ptr(uint64(1_000_000)),
		ExpiresAt:     ptr(int64(1757000000)),
		PostOnly:      true,
	}

	out := roundTripOpen(t, in)

	if *out != *in {
		// Pointer fields compare by address, so a difference here is checked field-wise below.
		if out.OrderID != in.OrderID || out.ClientOrderID != in.ClientOrderID ||
			out.MarketID != in.MarketID || out.UserID != in.UserID || out.Side != in.Side ||
			out.Type != in.Type || out.TimeInForce != in.TimeInForce || out.Price != in.Price ||
			out.Quantity != in.Quantity || out.PostOnly != in.PostOnly {
			t.Fatalf("scalar field lost:\n got %+v\nwant %+v", out, in)
		}
	}
	if out.QuoteQty == nil || *out.QuoteQty != *in.QuoteQty {
		t.Fatalf("quote_qty = %v, want %d", out.QuoteQty, *in.QuoteQty)
	}
	if out.ExpiresAt == nil || *out.ExpiresAt != *in.ExpiresAt {
		t.Fatalf("expires_at = %v, want %d", out.ExpiresAt, *in.ExpiresAt)
	}
}

// omitempty drops a nil pointer from the payload, so the decode has to give it back as nil rather
// than as a zero value. A zero ExpiresAt would make the order expire at the epoch — instantly.
func TestAbsentOptionalFieldsDecodeAsNil(t *testing.T) {
	in := &OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: SellOrder, Type: LimitOrder, TimeInForce: GoodTillCancel,
		Price: 100, Quantity: 10,
	}

	out := roundTripOpen(t, in)

	if out.QuoteQty != nil {
		t.Fatalf("absent quote_qty decoded as %d, want nil", *out.QuoteQty)
	}
	if out.ExpiresAt != nil {
		t.Fatalf("absent expires_at decoded as %d, want nil", *out.ExpiresAt)
	}
	if out.PostOnly {
		t.Fatal("absent post_only decoded as true")
	}
}

// A zero quote budget must not be silently indistinguishable from an absent one: the validator
// rejects the two for different reasons, so the wire format has to preserve which one was sent.
func TestZeroQuoteQtyIsNotDroppedAsAbsent(t *testing.T) {
	in := &OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: BuyOrder, Type: MarketOrder, TimeInForce: ImmediateOrCancel,
		QuoteQty: ptr(uint64(0)),
	}

	out := roundTripOpen(t, in)

	if out.QuoteQty == nil {
		t.Fatal("a zero quote_qty was dropped as absent")
	}
	if *out.QuoteQty != 0 {
		t.Fatalf("quote_qty = %d, want 0", *out.QuoteQty)
	}
}

func TestCancelOrderSurvivesTheWireRoundTrip(t *testing.T) {
	in := &CancelOrderEvent{
		OrderID:   uuid.MustParse("0199c0de-0000-7000-8000-000000000003"),
		MarketRef: "ETH-USDT",
	}

	event, err := NewCancelOrderEvent(in)
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != EventTypeCancelOrder {
		t.Fatalf("type = %q, want %q", event.Type, EventTypeCancelOrder)
	}

	raw, err := event.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseOrderEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := parsed.DecodeCancelOrder()
	if err != nil {
		t.Fatal(err)
	}

	if *out != *in {
		t.Fatalf("cancel event = %+v, want %+v", out, in)
	}
}

// The envelope's Type is what core switches on before it decodes anything, so it has to arrive
// intact and unambiguous.
func TestEnvelopeCarriesTheEventType(t *testing.T) {
	open, err := NewOpenOrderEvent(validLimit())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := open.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":"open_order"`) {
		t.Fatalf("envelope did not carry its type: %s", raw)
	}

	parsed, err := ParseOrderEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Type != EventTypeOpenOrder {
		t.Fatalf("type = %q, want %q", parsed.Type, EventTypeOpenOrder)
	}
}

// Garbage on the queue must come back as ErrParsingOrderEvent, which is what core matches on to
// dead-letter the delivery as malformed rather than crashing the matcher.
func TestParseOrderEventRejectsGarbage(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{"not json", []byte("this is not json at all")},
		{"truncated", []byte(`{"type":"open_order","payload":`)},
		{"a bare array", []byte(`[1,2,3]`)},
		{"empty", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseOrderEvent(tt.raw); !errors.Is(err, ErrParsingOrderEvent) {
				t.Fatalf("err = %v, want ErrParsingOrderEvent", err)
			}
		})
	}
}

// An unrecognised type parses cleanly — classify dead-letters it as ReasonUnknownType rather than
// the parser rejecting it, so a future event kind added by a newer API does not look like garbage.
func TestParseOrderEventAcceptsAnUnknownType(t *testing.T) {
	parsed, err := ParseOrderEvent([]byte(`{"type":"amend_order","payload":{}}`))
	if err != nil {
		t.Fatalf("unknown type should parse, got %v", err)
	}
	if parsed.Type == EventTypeOpenOrder || parsed.Type == EventTypeCancelOrder {
		t.Fatalf("unknown type was coerced to %q", parsed.Type)
	}
}

// Decoding the wrong shape has to fail rather than yield a zero-valued order: an open order decoded
// as a cancel would carry the nil UUID, and cancelling order 00000000-… is not a harmless no-op to
// discover at runtime.
func TestDecodeRejectsAMalformedPayload(t *testing.T) {
	event := &OrderEvent{Type: EventTypeOpenOrder, Payload: []byte(`{"price":"not a number"}`)}
	if _, err := event.DecodeOpenOrder(); !errors.Is(err, ErrParsingOrderEvent) {
		t.Fatalf("DecodeOpenOrder err = %v, want ErrParsingOrderEvent", err)
	}

	cancel := &OrderEvent{Type: EventTypeCancelOrder, Payload: []byte(`{"order_id":42}`)}
	if _, err := cancel.DecodeCancelOrder(); !errors.Is(err, ErrParsingOrderEvent) {
		t.Fatalf("DecodeCancelOrder err = %v, want ErrParsingOrderEvent", err)
	}
}

// An order that is valid going in must still be valid coming out. This is the property that keeps
// the API's accept and core's re-validate in agreement: if the wire format lost a field, core would
// dead-letter a command the API had already returned 201 for.
func TestARoundTrippedOrderStillValidates(t *testing.T) {
	constraints := MarketConstraints{
		PriceQuantum: 100, AmountQuantum: 10, MinOrderSize: 10,
		MaxOrderSize: 1_000_000_000, BaseScale: 1_000_000_000,
	}

	for _, order := range []*OpenOrderEvent{validLimit(), validMarketBuy(), validMarketSell()} {
		if err := ValidateOrderEvent(order, constraints); err != nil {
			t.Fatalf("fixture rejected before the round trip: %v", err)
		}
		if err := ValidateOrderEvent(roundTripOpen(t, order), constraints); err != nil {
			t.Fatalf("round-tripped order rejected: %v", err)
		}
	}
}

func roundTripOpen(t *testing.T, in *OpenOrderEvent) *OpenOrderEvent {
	t.Helper()

	event, err := NewOpenOrderEvent(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := event.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseOrderEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := parsed.DecodeOpenOrder()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// json.Marshal cannot fail on these structs, so the error returns exist only to satisfy the
// interface. Pinning the nil keeps a caller from treating a non-nil error as reachable.
func TestConstructorsDoNotErrorOnWellFormedInput(t *testing.T) {
	if _, err := NewOpenOrderEvent(&OpenOrderEvent{}); err != nil {
		t.Fatalf("NewOpenOrderEvent: %v", err)
	}
	if _, err := NewCancelOrderEvent(&CancelOrderEvent{}); err != nil {
		t.Fatalf("NewCancelOrderEvent: %v", err)
	}
	if _, err := json.Marshal(&OrderEvent{}); err != nil {
		t.Fatalf("OrderEvent must always marshal: %v", err)
	}
}
