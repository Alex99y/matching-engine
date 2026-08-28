package assert

import "testing"

func TestDiff(t *testing.T) {
	before := Balances{
		"USDT": {Symbol: "USDT", Balance: 1000, Blocked: 0},
		"ETH":  {Symbol: "ETH", Balance: 5, Blocked: 0},
	}
	after := Balances{
		"USDT": {Symbol: "USDT", Balance: 800, Blocked: 0},
		"ETH":  {Symbol: "ETH", Balance: 5, Blocked: 2}, // 2 moved to blocked
		"BTC":  {Symbol: "BTC", Balance: 3, Blocked: 0}, // symbol new in `after`
	}

	d := Diff(before, after)
	if d["USDT"] != (Move{Balance: -200}) {
		t.Errorf("USDT move = %+v", d["USDT"])
	}
	if d["ETH"] != (Move{Blocked: 2}) || d["ETH"].Net() != 2 {
		t.Errorf("ETH move = %+v", d["ETH"])
	}
	if d["BTC"] != (Move{Balance: 3}) {
		t.Errorf("BTC move = %+v", d["BTC"])
	}
}

func TestNetBySymbol(t *testing.T) {
	// Buyer spends 200 USDT, receives 2 ETH; seller mirrors. A fee-free trade nets zero.
	buyer := map[string]Move{
		"USDT": {Balance: -200},
		"ETH":  {Balance: 2},
	}
	seller := map[string]Move{
		"USDT": {Balance: 200},
		"ETH":  {Balance: -2},
	}
	net := netBySymbol([]map[string]Move{buyer, seller})
	if net["USDT"] != 0 || net["ETH"] != 0 {
		t.Fatalf("net = %v, want all zero", net)
	}

	// A 5-USDT fee leaves USDT short by 5.
	seller["USDT"] = Move{Balance: 195}
	net = netBySymbol([]map[string]Move{buyer, seller})
	if net["USDT"] != -5 {
		t.Fatalf("net[USDT] = %d, want -5", net["USDT"])
	}
}
