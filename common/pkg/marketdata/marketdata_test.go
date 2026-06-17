package marketdata

import "testing"

// An envelope must round-trip: marshal a typed payload, serialize, parse, and decode back to the
// same payload with the sequencing fields intact.
func TestEnvelopeRoundTrip(t *testing.T) {
	trade := Trade{Price: 100, Quantity: 5, TakerSide: "buy"}
	env, err := NewEnvelope("epoch-1", 42, EventTrade, "BTC-USDT", 1718000000000, trade)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	raw, err := env.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}

	got, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}
	if got.Epoch != "epoch-1" || got.Seq != 42 || got.Type != EventTrade || got.Market != "BTC-USDT" || got.Ts != 1718000000000 {
		t.Fatalf("envelope fields mismatch: %+v", got)
	}

	var decoded Trade
	if err := got.Decode(&decoded); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded != trade {
		t.Fatalf("payload = %+v, want %+v", decoded, trade)
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
