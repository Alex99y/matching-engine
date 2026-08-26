package command

import (
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/db/pkg/repository"
)

func TestMarketInputValidate(t *testing.T) {
	valid := func() marketInput {
		return marketInput{
			Name: "BTC-USDT", PriceQuantum: 1, AmountQuantum: 1000,
			MinOrderSize: 1000, MaxOrderSize: 2000, TakerFeeBps: 100, MakerFeeBps: 50,
		}
	}

	tests := []struct {
		name    string
		mutate  func(m *marketInput)
		wantErr error // nil means "any non-nil error" when wantAnyErr is true
	}{
		{"valid", func(m *marketInput) {}, nil},
		{"missing name", func(m *marketInput) { m.Name = "" }, errMarketNameRequired},
		{"non-positive price quantum", func(m *marketInput) { m.PriceQuantum = 0 }, errMarketPriceQuantumNonPositive},
		{"non-positive amount quantum", func(m *marketInput) { m.AmountQuantum = -1 }, errMarketAmountQuantumNonPositive},
		{"non-positive min order size", func(m *marketInput) { m.MinOrderSize = 0 }, errMarketMinOrderSizeNonPositive},
		{"non-positive max order size", func(m *marketInput) { m.MaxOrderSize = 0 }, errMarketMaxOrderSizeNonPositive},
		{"max less than min", func(m *marketInput) { m.MaxOrderSize = 999 }, errMarketMaxLtMin},
		{"fee below range", func(m *marketInput) { m.TakerFeeBps = -1 }, errMarketFeeOutOfRange},
		{"fee above range", func(m *marketInput) { m.MakerFeeBps = maxFeeBps + 1 }, errMarketFeeOutOfRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := valid()
			tt.mutate(&m)
			err := m.validate()
			if err != tt.wantErr {
				t.Errorf("validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}

	t.Run("invalid name format", func(t *testing.T) {
		m := valid()
		m.Name = "NOTAMARKET"
		if err := m.validate(); err == nil || !strings.Contains(err.Error(), "must be BASE-QUOTE") {
			t.Errorf("validate() = %v, want it to mention BASE-QUOTE", err)
		}
	})

	t.Run("min not a multiple of amount quantum", func(t *testing.T) {
		m := valid()
		m.MinOrderSize = 1001
		if err := m.validate(); err == nil || !strings.Contains(err.Error(), "must be a multiple of") {
			t.Errorf("validate() = %v, want it to mention multiple", err)
		}
	})

	t.Run("max not a multiple of amount quantum", func(t *testing.T) {
		m := valid()
		m.MaxOrderSize = 1999
		if err := m.validate(); err == nil || !strings.Contains(err.Error(), "must be a multiple of") {
			t.Errorf("validate() = %v, want it to mention multiple", err)
		}
	})
}

func TestMarketCreate_SingleFlags_Success(t *testing.T) {
	fake := &fakeMarketRepo{}
	setRepos(t, nil, fake, nil)

	out, err := runCommand(t, newMarketCreateCmd(),
		"--name", "btc-usdt",
		"--price_quantum", "1", "--amount_quantum", "1000",
		"--min_order_size", "1000", "--max_order_size", "2000",
		"--taker_fee_bps", "100", "--maker_fee_bps", "50")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "created market btc-usdt") {
		t.Errorf("stdout = %q, want it to contain the created market name", out)
	}

	if len(fake.createCalls) != 1 {
		t.Fatalf("createCalls = %d, want 1", len(fake.createCalls))
	}
	got := fake.createCalls[0]
	want := marketCreateCall{
		baseSymbol: "BTC", quoteSymbol: "USDT",
		priceQuantum: 1, amountQuantum: 1000, minOrderSize: 1000, maxOrderSize: 2000,
		takerFeeBps: 100, makerFeeBps: 50,
	}
	if got != want {
		t.Errorf("createCalls[0] = %+v, want %+v", got, want)
	}
}

func TestMarketCreate_JSONBatch_PartialFailure(t *testing.T) {
	fake := &fakeMarketRepo{
		createErrByName: map[string]error{
			"BTC-USDT": repository.ErrMarketAlreadyExists,
			"ETH-BTC":  repository.ErrInvalidInstruments,
		},
	}
	setRepos(t, nil, fake, nil)

	json := `[
		{"name":"BTC-USDT","price_quantum":1,"amount_quantum":1000,"min_order_size":1000,"max_order_size":2000},
		{"name":"ETH-BTC","price_quantum":1,"amount_quantum":1000,"min_order_size":1000,"max_order_size":2000},
		{"name":"ETH-USDT","price_quantum":1,"amount_quantum":1000,"min_order_size":1000,"max_order_size":2000}
	]`
	out, err := runCommand(t, newMarketCreateCmd(), "--json", json)

	if err == nil {
		t.Fatal("expected an error from the partial batch failure")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "already exists")
	}
	if !strings.Contains(err.Error(), "create them first") {
		t.Errorf("error = %q, want it to mention the invalid-instruments hint", err.Error())
	}
	if !strings.Contains(out, "created market ETH-USDT") {
		t.Errorf("stdout = %q, want it to contain the one successful create", out)
	}
}

func TestMarketCreate_InvalidJSON(t *testing.T) {
	setRepos(t, nil, &fakeMarketRepo{}, nil)

	_, err := runCommand(t, newMarketCreateCmd(), "--json", "not-json")
	if err == nil || !strings.Contains(err.Error(), "invalid --json") {
		t.Errorf("error = %v, want it to mention %q", err, "invalid --json")
	}
}

func TestMarketGet_ByName_InvalidFormat(t *testing.T) {
	setRepos(t, nil, &fakeMarketRepo{}, nil)

	_, err := runCommand(t, newMarketGetCmd(), "--name", "NOTAMARKET")
	if err == nil || !strings.Contains(err.Error(), "must be BASE-QUOTE") {
		t.Errorf("error = %v, want it to mention BASE-QUOTE", err)
	}
}

func TestMarketGet_ByName_NotFound(t *testing.T) {
	fake := &fakeMarketRepo{marketErr: repository.ErrMarketNotFound}
	setRepos(t, nil, fake, nil)

	_, err := runCommand(t, newMarketGetCmd(), "--name", "BTC-USDT")
	if err == nil || !strings.Contains(err.Error(), `"BTC-USDT" not found`) {
		t.Errorf("error = %v, want it to mention %q", err, `"BTC-USDT" not found`)
	}
}

func TestMarketGet_List(t *testing.T) {
	fake := &fakeMarketRepo{
		markets: []repository.Market{
			{ID: 1, BaseSymbol: "BTC", QuoteSymbol: "USDT"},
			{ID: 2, BaseSymbol: "ETH", QuoteSymbol: "USDT"},
		},
	}
	setRepos(t, nil, fake, nil)

	out, err := runCommand(t, newMarketGetCmd())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "BTC") || !strings.Contains(out, "ETH") {
		t.Errorf("stdout = %q, want it to contain both markets", out)
	}
}
