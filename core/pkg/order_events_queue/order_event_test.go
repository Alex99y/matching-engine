package order_events_queue

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	mev1 "github.com/alex99y/matching-engine/common/pkg/pb/me/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// The full journey of a command: the API builds it, serialises it onto the queue, and core parses
// it. Every field has to survive, and the optional ones are pointers precisely so that "absent"
// and "zero" stay distinguishable — a dropped ExpiresAt turns a temporary order into a permanent
// one, and a dropped QuoteQty leaves a market buy with no budget to spend.
func TestOpenOrderSurvivesTheWireRoundTrip(t *testing.T) {
	parent := uuid.MustParse("0199c0de-0000-7000-8000-000000000009")
	in := &OpenOrderEvent{
		OrderID:         uuid.MustParse("0199c0de-0000-7000-8000-000000000001"),
		ClientOrderID:   "my-order-1",
		MarketID:        7,
		UserID:          uuid.MustParse("0199c0de-0000-7000-8000-000000000002"),
		Side:            BuyOrder,
		Type:            LimitOrder,
		TimeInForce:     GoodTillCancel,
		Price:           79_200_000_000,
		Quantity:        500_000_000,
		QuoteQty:        ptr(uint64(1_000_000)),
		ExpiresAt:       ptr(int64(1757000000)),
		PostOnly:        true,
		TakeProfitPrice: ptr(uint64(80_000_000_000)),
		StopLossPrice:   ptr(uint64(78_000_000_000)),
		ParentOrderID:   &parent,
	}

	out := roundTripOpen(t, in)

	if out.OrderID != in.OrderID || out.ClientOrderID != in.ClientOrderID ||
		out.MarketID != in.MarketID || out.UserID != in.UserID || out.Side != in.Side ||
		out.Type != in.Type || out.TimeInForce != in.TimeInForce || out.Price != in.Price ||
		out.Quantity != in.Quantity || out.PostOnly != in.PostOnly {
		t.Fatalf("scalar field lost:\n got %+v\nwant %+v", out, in)
	}
	for name, got := range map[string][2]*uint64{
		"quote_qty":         {out.QuoteQty, in.QuoteQty},
		"take_profit_price": {out.TakeProfitPrice, in.TakeProfitPrice},
		"stop_loss_price":   {out.StopLossPrice, in.StopLossPrice},
	} {
		if got[0] == nil || *got[0] != *got[1] {
			t.Fatalf("%s = %v, want %d", name, got[0], *got[1])
		}
	}
	if out.ExpiresAt == nil || *out.ExpiresAt != *in.ExpiresAt {
		t.Fatalf("expires_at = %v, want %d", out.ExpiresAt, *in.ExpiresAt)
	}
	if out.ParentOrderID == nil || *out.ParentOrderID != parent {
		t.Fatalf("parent_order_id = %v, want %s", out.ParentOrderID, parent)
	}
}

// Every enum value must map to the wire and back; a value that only round-trips by accident
// (through the unspecified member) would be rejected by the validator after a 201.
func TestEnumsSurviveTheWireRoundTrip(t *testing.T) {
	for _, side := range []OrderSide{BuyOrder, SellOrder} {
		for _, typ := range []OrderType{LimitOrder, MarketOrder} {
			for _, tif := range []TimeInForce{GoodTillCancel, ImmediateOrCancel, FillOrKill} {
				out := roundTripOpen(t, &OpenOrderEvent{Side: side, Type: typ, TimeInForce: tif})
				if out.Side != side || out.Type != typ || out.TimeInForce != tif {
					t.Fatalf("(%s, %s, %s) came back as (%s, %s, %s)", side, typ, tif, out.Side, out.Type, out.TimeInForce)
				}
			}
		}
	}
}

// A nil pointer must come back as nil rather than as a zero value. A zero ExpiresAt would make
// the order expire at the epoch — instantly.
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
	if out.TakeProfitPrice != nil || out.StopLossPrice != nil || out.ParentOrderID != nil {
		t.Fatalf("absent bracket fields decoded as %v/%v/%v, want nil", out.TakeProfitPrice, out.StopLossPrice, out.ParentOrderID)
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

	event := NewCancelOrderEvent(in)
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
	if parsed.Type != EventTypeCancelOrder || parsed.Open != nil || parsed.Cancel == nil {
		t.Fatalf("parsed = %+v, want a cancel", parsed)
	}
	if *parsed.Cancel != *in {
		t.Fatalf("cancel event = %+v, want %+v", parsed.Cancel, in)
	}
}

// The wire carries the schema version first so a consumer can pick a decoder before it decodes.
func TestEveryBodyCarriesTheSchemaVersion(t *testing.T) {
	for _, event := range []*OrderEvent{
		NewOpenOrderEvent(validLimit()),
		NewCancelOrderEvent(&CancelOrderEvent{OrderID: uuid.New(), MarketRef: "ETH-USDT"}),
	} {
		raw, err := event.ToBytes()
		if err != nil {
			t.Fatal(err)
		}
		var command mev1.OrderCommand
		if err := proto.Unmarshal(raw, &command); err != nil {
			t.Fatal(err)
		}
		if command.GetVersion() != mev1.Version {
			t.Fatalf("%s body carries version %d, want %d", event.Type, command.GetVersion(), mev1.Version)
		}
	}
}

// Garbage on the queue must come back as ErrParsingOrderEvent, which is what core matches on to
// dead-letter the delivery as malformed rather than crashing the matcher.
func TestParseOrderEventRejectsGarbage(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{"truncated varint", []byte{0x80}},
		{"invalid wire type", []byte("not protobuf")},
		{"empty", nil},
		{"order id of the wrong length", openWithOrderID(t, []byte{1, 2, 3})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseOrderEvent(tt.raw); !errors.Is(err, ErrParsingOrderEvent) {
				t.Fatalf("err = %v, want ErrParsingOrderEvent", err)
			}
		})
	}
}

// A body of another schema version is refused by name, so the dead-letter row says which decoder
// is missing rather than "invalid".
func TestParseOrderEventNamesAnUnsupportedVersion(t *testing.T) {
	raw, err := proto.Marshal(&mev1.OrderCommand{Version: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseOrderEvent(raw)
	if !errors.Is(err, ErrParsingOrderEvent) || !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("err = %v, want ErrParsingOrderEvent wrapping ErrUnsupportedVersion", err)
	}
	if !strings.HasSuffix(err.Error(), "unsupported schema version 2") {
		t.Fatalf("err = %q, want it to end with the version", err)
	}
}

// A command this build does not know parses cleanly with neither member set — classify
// dead-letters it as ReasonUnknownType rather than the parser rejecting it, so a future command
// added by a newer API does not look like garbage.
func TestParseOrderEventAcceptsAnUnknownCommand(t *testing.T) {
	empty, err := proto.Marshal(&mev1.OrderCommand{Version: mev1.Version})
	if err != nil {
		t.Fatal(err)
	}
	future := protowire.AppendTag(empty, 99, protowire.BytesType)
	future = protowire.AppendBytes(future, []byte{0x08, 0x01})

	for name, raw := range map[string][]byte{"no member": empty, "unknown member": future} {
		t.Run(name, func(t *testing.T) {
			parsed, err := ParseOrderEvent(raw)
			if err != nil {
				t.Fatalf("an unknown command should parse, got %v", err)
			}
			if parsed.Type != "" || parsed.Open != nil || parsed.Cancel != nil {
				t.Fatalf("unknown command was coerced to %+v", parsed)
			}
		})
	}
}

// An absent id decodes as the nil UUID so the validator reports it as missing, exactly as a JSON
// body without the field used to be reported.
func TestAbsentUUIDsDecodeAsNil(t *testing.T) {
	raw, err := proto.Marshal(&mev1.OrderCommand{
		Version: mev1.Version,
		Command: &mev1.OrderCommand_Open{Open: &mev1.OpenOrder{Price: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseOrderEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Open.OrderID != uuid.Nil || parsed.Open.UserID != uuid.Nil {
		t.Fatalf("absent ids decoded as %s/%s", parsed.Open.OrderID, parsed.Open.UserID)
	}
	if err := ValidateOrderEvent(parsed.Open, MarketConstraints{}); !errors.Is(err, ErrInvalidOrderEvent) {
		t.Fatalf("validator err = %v, want ErrInvalidOrderEvent", err)
	}
}

// An enum value this build does not know must not collapse into a known one; it decodes to its
// wire name so the validator's rejection says what arrived.
func TestUnknownEnumValueIsNamedNotCoerced(t *testing.T) {
	raw, err := proto.Marshal(&mev1.OrderCommand{
		Version: mev1.Version,
		Command: &mev1.OrderCommand_Open{Open: &mev1.OpenOrder{Side: mev1.OrderSide(7)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseOrderEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Open.Side == BuyOrder || parsed.Open.Side == SellOrder || parsed.Open.Side == "" {
		t.Fatalf("unknown side decoded as %q", parsed.Open.Side)
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

// The JSON rendering is what an operator reads in dead_letters.payload: it must name the schema
// version and the fields as the .proto does, and refuse the same bodies the parser refuses.
func TestCommandJSONRendersWhatTheParserAccepts(t *testing.T) {
	order := validLimit()
	raw, err := NewOpenOrderEvent(order).ToBytes()
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := CommandJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version uint32 `json:"version"`
		Open    struct {
			Price uint64 `json:"price,string"`
		} `json:"open"`
	}
	if err := json.Unmarshal(rendered, &got); err != nil {
		t.Fatalf("rendering is not JSON: %v\n%s", err, rendered)
	}
	if got.Version != mev1.Version || got.Open.Price != order.Price {
		t.Fatalf("rendering = %s, want version %d and price %d", rendered, mev1.Version, order.Price)
	}

	if _, err := CommandJSON([]byte{0x80}); !errors.Is(err, ErrParsingOrderEvent) {
		t.Fatalf("garbage rendered, err = %v", err)
	}
	v2, err := proto.Marshal(&mev1.OrderCommand{Version: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommandJSON(v2); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("another version rendered as v1, err = %v", err)
	}
}

func roundTripOpen(t *testing.T, in *OpenOrderEvent) *OpenOrderEvent {
	t.Helper()

	raw, err := NewOpenOrderEvent(in).ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseOrderEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Type != EventTypeOpenOrder || parsed.Open == nil || parsed.Cancel != nil {
		t.Fatalf("parsed = %+v, want an open order", parsed)
	}
	return parsed.Open
}

func openWithOrderID(t *testing.T, id []byte) []byte {
	t.Helper()
	raw, err := proto.Marshal(&mev1.OrderCommand{
		Version: mev1.Version,
		Command: &mev1.OrderCommand_Open{Open: &mev1.OpenOrder{OrderId: id}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
