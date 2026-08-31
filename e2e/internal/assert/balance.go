package assert

import (
	"context"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// Balances is one account's balance rows keyed by symbol.
type Balances map[string]client.Balance

// Snapshot reads the account's balances into a Balances map.
func Snapshot(ctx context.Context, c *client.Client, token string) (Balances, error) {
	rows, err := c.GetBalances(ctx, token)
	if err != nil {
		return nil, err
	}
	out := make(Balances, len(rows))
	for _, r := range rows {
		out[r.Symbol] = r
	}
	return out, nil
}

// Move is (after - before) for one symbol. Net is the combined balance+blocked change: a
// pure transfer nets to zero, a fee makes it negative.
type Move struct {
	Balance int64
	Blocked int64
}

func (m Move) Net() int64 { return m.Balance + m.Blocked }

// Diff returns per-symbol (after - before) for every symbol in either snapshot.
func Diff(before, after Balances) map[string]Move {
	out := map[string]Move{}
	for sym := range before {
		out[sym] = Move{}
	}
	for sym := range after {
		out[sym] = Move{}
	}
	for sym := range out {
		b, a := before[sym], after[sym]
		out[sym] = Move{Balance: a.Balance - b.Balance, Blocked: a.Blocked - b.Blocked}
	}
	return out
}

// NetZero asserts that, summed across every supplied diff, each symbol's net
// (balance+blocked) movement is zero — funds were only transferred, no fees taken. Use it on
// fee-free markets; for markets with fees, assert per-symbol net == -feePaid directly.
func NetZero(t testing.TB, diffs ...map[string]Move) {
	t.Helper()
	for sym, n := range netBySymbol(diffs) {
		if n != 0 {
			t.Fatalf("assert.NetZero: %s net movement across accounts = %d, want 0", sym, n)
		}
	}
}

func netBySymbol(diffs []map[string]Move) map[string]int64 {
	net := map[string]int64{}
	for _, d := range diffs {
		for sym, m := range d {
			net[sym] += m.Net()
		}
	}
	return net
}
