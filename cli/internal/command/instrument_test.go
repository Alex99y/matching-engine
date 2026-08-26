package command

import (
	"strings"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/db/pkg/repository"
)

func TestInstrumentInputValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   instrumentInput
		wantErr error
	}{
		{"valid", instrumentInput{Name: "Bitcoin", Symbol: "BTC", Decimals: 8}, nil},
		{"missing name", instrumentInput{Symbol: "BTC", Decimals: 8}, errInstrumentNameRequired},
		{"name too long", instrumentInput{Name: strings.Repeat("a", 101), Symbol: "BTC", Decimals: 8}, errInstrumentNameTooLong},
		{"missing symbol", instrumentInput{Name: "Bitcoin", Decimals: 8}, errInstrumentSymbolRequired},
		{"symbol too long", instrumentInput{Name: "Bitcoin", Symbol: "ABCDEFGHKLM", Decimals: 8}, errInstrumentSymbolTooLong},
		{"decimals negative", instrumentInput{Name: "Bitcoin", Symbol: "BTC", Decimals: -1}, errInstrumentDecimalsInvalid},
		{"decimals too large", instrumentInput{Name: "Bitcoin", Symbol: "BTC", Decimals: 19}, errInstrumentDecimalsInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.validate()
			if err != tt.wantErr {
				t.Errorf("validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestInstrumentInputNormalize(t *testing.T) {
	inp := instrumentInput{Name: "  Bitcoin  ", Symbol: "  btc  "}
	inp.normalize()
	if inp.Name != "Bitcoin" {
		t.Errorf("Name = %q, want %q", inp.Name, "Bitcoin")
	}
	if inp.Symbol != "BTC" {
		t.Errorf("Symbol = %q, want %q", inp.Symbol, "BTC")
	}
}

func TestInstrumentCreate_SingleFlags_Success(t *testing.T) {
	fake := &fakeInstrumentRepo{}
	setRepos(t, fake, nil, nil)

	out, err := runCommand(t, newInstrumentCreateCmd(), "--name", "  Bitcoin  ", "--symbol", "  btc  ", "--decimals", "8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "created instrument BTC") {
		t.Errorf("stdout = %q, want it to contain %q", out, "created instrument BTC")
	}

	if len(fake.createCalls) != 1 {
		t.Fatalf("createCalls = %d, want 1", len(fake.createCalls))
	}
	got := fake.createCalls[0]
	want := instrumentCreateCall{name: "Bitcoin", symbol: "BTC", decimals: 8}
	if got != want {
		t.Errorf("createCalls[0] = %+v, want %+v", got, want)
	}
}

func TestInstrumentCreate_JSONBatch_PartialFailure(t *testing.T) {
	fake := &fakeInstrumentRepo{
		createErrBySymbol: map[string]error{
			"USDT": repository.ErrInstrumentAlreadyExists,
		},
	}
	setRepos(t, fake, nil, nil)

	json := `[{"name":"Bitcoin","symbol":"BTC","decimals":8},{"name":"Tether","symbol":"USDT","decimals":6},{"name":"","symbol":"BAD","decimals":0}]`
	out, err := runCommand(t, newInstrumentCreateCmd(), "--json", json)

	if err == nil {
		t.Fatal("expected an error from the partial batch failure")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "already exists")
	}
	if !strings.Contains(err.Error(), errInstrumentNameRequired.Error()) {
		t.Errorf("error = %q, want it to mention %q", err.Error(), errInstrumentNameRequired.Error())
	}
	if !strings.Contains(out, "created instrument BTC") {
		t.Errorf("stdout = %q, want it to contain the one successful create", out)
	}

	if len(fake.createCalls) != 2 {
		t.Fatalf("createCalls = %d, want 2 (BTC and USDT; the invalid entry never reaches the repo)", len(fake.createCalls))
	}
}

func TestInstrumentCreate_InvalidJSON(t *testing.T) {
	setRepos(t, &fakeInstrumentRepo{}, nil, nil)

	_, err := runCommand(t, newInstrumentCreateCmd(), "--json", "not-json")
	if err == nil || !strings.Contains(err.Error(), "invalid --json") {
		t.Errorf("error = %v, want it to mention %q", err, "invalid --json")
	}
}

func TestInstrumentGet_BySymbol_NotFound(t *testing.T) {
	fake := &fakeInstrumentRepo{instrumentErr: repository.ErrInstrumentNotFound}
	setRepos(t, fake, nil, nil)

	_, err := runCommand(t, newInstrumentGetCmd(), "--symbol", "btc")
	if err == nil || !strings.Contains(err.Error(), `"BTC" not found`) {
		t.Errorf("error = %v, want it to mention %q", err, `"BTC" not found`)
	}
}

func TestInstrumentGet_BySymbol_Found(t *testing.T) {
	fake := &fakeInstrumentRepo{
		instrument: &repository.Instrument{ID: 1, Name: "Bitcoin", Symbol: "BTC", Decimals: 8, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	setRepos(t, fake, nil, nil)

	out, err := runCommand(t, newInstrumentGetCmd(), "--symbol", "btc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "BTC") || !strings.Contains(out, "Bitcoin") {
		t.Errorf("stdout = %q, want it to contain the instrument row", out)
	}
}

func TestInstrumentGet_List(t *testing.T) {
	fake := &fakeInstrumentRepo{
		instruments: []repository.Instrument{
			{ID: 1, Name: "Bitcoin", Symbol: "BTC", Decimals: 8},
			{ID: 2, Name: "Tether", Symbol: "USDT", Decimals: 6},
		},
	}
	setRepos(t, fake, nil, nil)

	out, err := runCommand(t, newInstrumentGetCmd())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "BTC") || !strings.Contains(out, "USDT") {
		t.Errorf("stdout = %q, want it to contain both instruments", out)
	}
}
