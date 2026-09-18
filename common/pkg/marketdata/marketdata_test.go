package marketdata

import (
	"errors"
	"reflect"
	"testing"

	mev1 "github.com/alex99y/matching-engine/common/pkg/pb/me/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// Every payload type must round-trip: build the envelope, serialize, parse, and get the same
// payload back with the sequencing fields intact and the type derived from the payload.
func TestEnvelopeRoundTrip(t *testing.T) {
	orderID := uuid.New()
	for _, payload := range []Payload{
		Trade{Price: 100, Quantity: 5, TakerSide: "buy"},
		Book{Side: "sell", Price: 101, Quantity: 0},
		Heartbeat{},
		Snapshot{Epoch: "epoch-1", Seq: 42, Market: "BTC-USDT", Bids: []BookLevel{{100, 5}, {99, 1}}, Asks: []BookLevel{{101, 7}}},
		OrderUpdate{OrderID: orderID, Status: "partially_filled", Filled: 2, Remaining: 3},
	} {
		t.Run(string(payload.eventType()), func(t *testing.T) {
			env := NewEnvelope("epoch-1", 42, "BTC-USDT", 1718000000000, payload)
			if env.Type != payload.eventType() {
				t.Fatalf("type = %q, want %q", env.Type, payload.eventType())
			}

			raw, err := env.ToBytes()
			if err != nil {
				t.Fatalf("ToBytes: %v", err)
			}
			got, err := ParseEnvelope(raw)
			if err != nil {
				t.Fatalf("ParseEnvelope: %v", err)
			}
			if got.Epoch != "epoch-1" || got.Seq != 42 || got.Type != env.Type || got.Market != "BTC-USDT" || got.Ts != 1718000000000 {
				t.Fatalf("envelope fields mismatch: %+v", got)
			}
			if !reflect.DeepEqual(got.Payload, payload) {
				t.Fatalf("payload = %#v, want %#v", got.Payload, payload)
			}
		})
	}
}

// A body this build cannot decode is refused by name, so the consumer's log says which it was.
func TestParseEnvelopeRejectsWhatItCannotDecode(t *testing.T) {
	v2, err := proto.Marshal(&mev1.Event{Version: 2})
	if err != nil {
		t.Fatal(err)
	}
	noMember, err := proto.Marshal(&mev1.Event{Version: mev1.Version, Epoch: "e1"})
	if err != nil {
		t.Fatal(err)
	}
	badOrderID, err := proto.Marshal(&mev1.Event{
		Version: mev1.Version,
		Payload: &mev1.Event_Order{Order: &mev1.OrderUpdate{OrderId: []byte{1, 2}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name string
		raw  []byte
		want error
	}{
		{"garbage", []byte{0x80}, ErrParsingEnvelope},
		{"another version", v2, ErrUnsupportedVersion},
		{"no payload member", noMember, ErrParsingEnvelope},
		{"order id that is not a uuid", badOrderID, ErrParsingEnvelope},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEnvelope(tt.raw)
			if !errors.Is(err, ErrParsingEnvelope) || !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestUserIDFromKey(t *testing.T) {
	uid := "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	cases := []struct {
		key    string
		want   string
		wantOK bool
	}{
		{PrivateKey(uid, EventOrder), uid, true}, // user.<uid>.order
		{UserBinding(uid), uid, true},            // user.<uid>.#
		{PublicKey("BTC-USDT", EventTrade), "", false},
		{"user.", "", false},
		{"user", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := UserIDFromKey(c.key)
		if got != c.want || ok != c.wantOK {
			t.Errorf("UserIDFromKey(%q) = (%q, %v), want (%q, %v)", c.key, got, ok, c.want, c.wantOK)
		}
	}
}

func TestRoutingKeys(t *testing.T) {
	cases := []struct{ got, want string }{
		{PublicKey("BTC-USDT", EventTrade), "market.BTC-USDT.trade"},
		{PublicKey("BTC-USDT", EventBook), "market.BTC-USDT.book"},
		{PrivateKey("u1", EventOrder), "user.u1.order"},
		{MarketBinding("BTC-USDT"), "market.BTC-USDT.#"},
		{UserBinding("u1"), "user.u1.#"},
		{TypeBinding(EventTrade), "market.*.trade"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("routing key = %q, want %q", c.got, c.want)
		}
	}
}
