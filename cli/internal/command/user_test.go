package command

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/alex99y/matching-engine/db/pkg/repository"
)

func TestUserFreeze_Success(t *testing.T) {
	userID := uuid.New()
	fakeUsers := &fakeUserRepo{usersByUsername: map[string]*repository.User{
		"alice": {ID: userID, Username: "alice"},
	}}
	setRepos(t, nil, nil, fakeUsers)

	out, err := runCommand(t, newUserFreezeCmd(), "--username", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "froze account for user alice") {
		t.Errorf("stdout = %q, want it to confirm the freeze", out)
	}
	if len(fakeUsers.freezeCalls) != 1 || fakeUsers.freezeCalls[0] != userID {
		t.Errorf("freezeCalls = %v, want [%v]", fakeUsers.freezeCalls, userID)
	}
}

func TestUserFreeze_MissingUsername(t *testing.T) {
	setRepos(t, nil, nil, &fakeUserRepo{})

	_, err := runCommand(t, newUserFreezeCmd())
	if err != errUserBalanceUsernameRequired {
		t.Errorf("error = %v, want %v", err, errUserBalanceUsernameRequired)
	}
}

func TestUserFreeze_UserNotFound(t *testing.T) {
	setRepos(t, nil, nil, &fakeUserRepo{})

	_, err := runCommand(t, newUserFreezeCmd(), "--username", "ghost")
	if err == nil || !strings.Contains(err.Error(), `"ghost" not found`) {
		t.Errorf("error = %v, want it to mention %q", err, `"ghost" not found`)
	}
}

func TestUserUnfreeze_Success(t *testing.T) {
	userID := uuid.New()
	fakeUsers := &fakeUserRepo{usersByUsername: map[string]*repository.User{
		"alice": {ID: userID, Username: "alice"},
	}}
	setRepos(t, nil, nil, fakeUsers)

	out, err := runCommand(t, newUserUnfreezeCmd(), "--username", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "unfroze account for user alice") {
		t.Errorf("stdout = %q, want it to confirm the unfreeze", out)
	}
	if len(fakeUsers.unfreezeCalls) != 1 || fakeUsers.unfreezeCalls[0] != userID {
		t.Errorf("unfreezeCalls = %v, want [%v]", fakeUsers.unfreezeCalls, userID)
	}
}

func TestUserBalanceAdd_Success(t *testing.T) {
	userID := uuid.New()
	fakeUsers := &fakeUserRepo{usersByUsername: map[string]*repository.User{
		"alice": {ID: userID, Username: "alice"},
	}}
	fakeInstruments := &fakeInstrumentRepo{instrument: &repository.Instrument{ID: 7, Symbol: "BTC"}}
	setRepos(t, fakeInstruments, nil, fakeUsers)

	out, err := runCommand(t, newUserBalanceAddCmd(),
		"--username", "alice", "--instrument", "btc", "--amount", "100000", "--reason", "faucet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "added 100000 to BTC balance for user alice") {
		t.Errorf("stdout = %q, want it to confirm the balance add", out)
	}

	if len(fakeUsers.addBalanceCalls) != 1 {
		t.Fatalf("addBalanceCalls = %d, want 1", len(fakeUsers.addBalanceCalls))
	}
	call := fakeUsers.addBalanceCalls[0]
	if call.userID != userID || call.instrumentID != 7 || call.amount != 100000 {
		t.Errorf("addBalanceCalls[0] = %+v, want userID=%v instrumentID=7 amount=100000", call, userID)
	}
	if call.reason == nil || *call.reason != "faucet" {
		t.Errorf("reason = %v, want %q", call.reason, "faucet")
	}
}

func TestUserBalanceAdd_BlankReasonBecomesNil(t *testing.T) {
	userID := uuid.New()
	fakeUsers := &fakeUserRepo{usersByUsername: map[string]*repository.User{"alice": {ID: userID}}}
	fakeInstruments := &fakeInstrumentRepo{instrument: &repository.Instrument{ID: 7, Symbol: "BTC"}}
	setRepos(t, fakeInstruments, nil, fakeUsers)

	_, err := runCommand(t, newUserBalanceAddCmd(), "--username", "alice", "--instrument", "btc", "--amount", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeUsers.addBalanceCalls[0].reason != nil {
		t.Errorf("reason = %v, want nil", fakeUsers.addBalanceCalls[0].reason)
	}
}

func TestUserBalanceAdd_UserNotFound(t *testing.T) {
	fakeInstruments := &fakeInstrumentRepo{instrument: &repository.Instrument{ID: 7, Symbol: "BTC"}}
	setRepos(t, fakeInstruments, nil, &fakeUserRepo{})

	_, err := runCommand(t, newUserBalanceAddCmd(), "--username", "ghost", "--instrument", "btc", "--amount", "1")
	if err == nil || !strings.Contains(err.Error(), `"ghost" not found`) {
		t.Errorf("error = %v, want it to mention %q", err, `"ghost" not found`)
	}
}

func TestUserBalanceAdd_InstrumentNotFound(t *testing.T) {
	fakeUsers := &fakeUserRepo{usersByUsername: map[string]*repository.User{"alice": {ID: uuid.New()}}}
	fakeInstruments := &fakeInstrumentRepo{instrumentErr: repository.ErrInstrumentNotFound}
	setRepos(t, fakeInstruments, nil, fakeUsers)

	_, err := runCommand(t, newUserBalanceAddCmd(), "--username", "alice", "--instrument", "ghost", "--amount", "1")
	if err == nil || !strings.Contains(err.Error(), `"GHOST" not found`) {
		t.Errorf("error = %v, want it to mention %q", err, `"GHOST" not found`)
	}
}

func TestUserBalanceAdd_ValidationErrors(t *testing.T) {
	setRepos(t, &fakeInstrumentRepo{}, nil, &fakeUserRepo{})

	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{
		{"missing username", []string{"--instrument", "BTC", "--amount", "1"}, errUserBalanceUsernameRequired},
		{"missing instrument", []string{"--username", "alice", "--amount", "1"}, errUserBalanceInstrumentRequired},
		{"non-positive amount", []string{"--username", "alice", "--instrument", "BTC", "--amount", "0"}, errUserBalanceAmountNonPositive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runCommand(t, newUserBalanceAddCmd(), tt.args...)
			if err != tt.wantErr {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestUserBalanceRemove_InsufficientBalance(t *testing.T) {
	fakeUsers := &fakeUserRepo{
		usersByUsername:  map[string]*repository.User{"alice": {ID: uuid.New()}},
		removeBalanceErr: repository.ErrInsufficientBalance,
	}
	fakeInstruments := &fakeInstrumentRepo{instrument: &repository.Instrument{ID: 7, Symbol: "BTC"}}
	setRepos(t, fakeInstruments, nil, fakeUsers)

	_, err := runCommand(t, newUserBalanceRemoveCmd(), "--username", "alice", "--instrument", "btc", "--amount", "1")
	if err == nil || !strings.Contains(err.Error(), "does not have sufficient BTC balance") {
		t.Errorf("error = %v, want it to mention insufficient balance", err)
	}
}

func TestUserBalanceFreeze_InsufficientBalance(t *testing.T) {
	fakeUsers := &fakeUserRepo{
		usersByUsername:  map[string]*repository.User{"alice": {ID: uuid.New()}},
		freezeBalanceErr: repository.ErrInsufficientBalance,
	}
	fakeInstruments := &fakeInstrumentRepo{instrument: &repository.Instrument{ID: 7, Symbol: "BTC"}}
	setRepos(t, fakeInstruments, nil, fakeUsers)

	_, err := runCommand(t, newUserBalanceFreezeCmd(), "--username", "alice", "--instrument", "btc", "--amount", "1")
	if err == nil || !strings.Contains(err.Error(), "sufficient BTC balance to freeze") {
		t.Errorf("error = %v, want it to mention insufficient balance to freeze", err)
	}
}

func TestUserBalanceUnfreeze_InsufficientFrozen(t *testing.T) {
	fakeUsers := &fakeUserRepo{
		usersByUsername:    map[string]*repository.User{"alice": {ID: uuid.New()}},
		unfreezeBalanceErr: repository.ErrInsufficientFrozen,
	}
	fakeInstruments := &fakeInstrumentRepo{instrument: &repository.Instrument{ID: 7, Symbol: "BTC"}}
	setRepos(t, fakeInstruments, nil, fakeUsers)

	_, err := runCommand(t, newUserBalanceUnfreezeCmd(), "--username", "alice", "--instrument", "btc", "--amount", "1")
	if err == nil || !strings.Contains(err.Error(), "sufficient frozen BTC balance to unfreeze") {
		t.Errorf("error = %v, want it to mention insufficient frozen balance", err)
	}
}
