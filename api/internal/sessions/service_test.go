package sessions_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alex99y/matching-engine/api/internal/sessions"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/sessionscope"
	"github.com/alex99y/matching-engine/common/pkg/token"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type revokeCall struct {
	userID    uuid.UUID
	tokenHash string
}

type fakeSessionRepository struct {
	insertErr   error
	insertCalls []repository.InsertSessionParams

	sessionByHash map[string]*repository.Session
	getByHashErr  error

	revokeErr   error
	revokeCalls []revokeCall

	activeSessions []repository.Session
	activeErr      error

	refreshErr   error
	refreshCalls []revokeCall
}

func (f *fakeSessionRepository) InsertSession(ctx context.Context, params repository.InsertSessionParams) error {
	f.insertCalls = append(f.insertCalls, params)
	return f.insertErr
}

func (f *fakeSessionRepository) GetActiveSessionByTokenHash(ctx context.Context, tokenHash string) (*repository.Session, error) {
	if f.getByHashErr != nil {
		return nil, f.getByHashErr
	}
	if s, ok := f.sessionByHash[tokenHash]; ok {
		return s, nil
	}
	return nil, repository.ErrSessionNotFound
}

func (f *fakeSessionRepository) RevokeSessionByTokenHash(ctx context.Context, userID uuid.UUID, tokenHash string) error {
	f.revokeCalls = append(f.revokeCalls, revokeCall{userID, tokenHash})
	return f.revokeErr
}

func (f *fakeSessionRepository) GetActiveSessionByUserId(ctx context.Context, userID uuid.UUID) ([]repository.Session, error) {
	return f.activeSessions, f.activeErr
}

func (f *fakeSessionRepository) RefreshSession(ctx context.Context, userID uuid.UUID, tokenHash string, newExpiresAt, minCreatedAt time.Time) error {
	f.refreshCalls = append(f.refreshCalls, revokeCall{userID, tokenHash})
	return f.refreshErr
}

func newTestService(repo sessions.SessionRepository) *sessions.SessionService {
	return sessions.NewSessionService(logger.NewLogger(logger.Error), repo)
}

func TestCreateSessionInsertsLoginWriteSession(t *testing.T) {
	repo := &fakeSessionRepository{}
	svc := newTestService(repo)
	userID := uuid.New()

	rawToken, err := svc.CreateSession(context.Background(), userID, nil, nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if rawToken == "" {
		t.Fatal("rawToken is empty")
	}
	if len(repo.insertCalls) != 1 {
		t.Fatalf("insertCalls = %d, want 1", len(repo.insertCalls))
	}
	call := repo.insertCalls[0]
	if call.UserID != userID || call.Origin != sessionscope.OriginLogin || call.Scope != sessionscope.ScopeWrite {
		t.Fatalf("insertCalls[0] = %+v, want userID=%v origin=login scope=write", call, userID)
	}
	if call.TokenHash != token.Hash(rawToken) {
		t.Fatal("stored token hash does not match the hash of the returned raw token")
	}
}

func TestCreateSessionRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeSessionRepository{insertErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.CreateSession(context.Background(), uuid.New(), nil, nil)
	if !errors.Is(err, sessions.ErrCreateSession) {
		t.Fatalf("err = %v, want ErrCreateSession", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestMintTokenInsertsMintedSessionWithRequestedScope(t *testing.T) {
	repo := &fakeSessionRepository{}
	svc := newTestService(repo)
	userID := uuid.New()

	rawToken, expiresAt, err := svc.MintToken(context.Background(), userID, sessionscope.ScopeRead, nil, nil)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if rawToken == "" || expiresAt.IsZero() {
		t.Fatalf("rawToken=%q expiresAt=%v, want both set", rawToken, expiresAt)
	}
	if len(repo.insertCalls) != 1 {
		t.Fatalf("insertCalls = %d, want 1", len(repo.insertCalls))
	}
	call := repo.insertCalls[0]
	if call.Origin != sessionscope.OriginMinted || call.Scope != sessionscope.ScopeRead {
		t.Fatalf("insertCalls[0] = %+v, want origin=minted scope=read", call)
	}
}

func TestValidateTokenSuccess(t *testing.T) {
	userID := uuid.New()
	rawToken := "raw-test-token"
	repo := &fakeSessionRepository{sessionByHash: map[string]*repository.Session{
		token.Hash(rawToken): {UserID: userID, Origin: sessionscope.OriginLogin, Scope: sessionscope.ScopeWrite, Frozen: true},
	}}
	svc := newTestService(repo)

	info, err := svc.ValidateToken(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if info.UserID != userID || info.Origin != sessionscope.OriginLogin || info.Scope != sessionscope.ScopeWrite || !info.Frozen {
		t.Fatalf("info = %+v, unexpected mapping", info)
	}
}

func TestValidateTokenNotFoundReturnsMiddlewareSentinel(t *testing.T) {
	svc := newTestService(&fakeSessionRepository{})

	_, err := svc.ValidateToken(context.Background(), "unknown-token")
	if !errors.Is(err, middleware.ErrInvalidSession) {
		t.Fatalf("err = %v, want middleware.ErrInvalidSession", err)
	}
}

// Unlike every sibling method in this file, ValidateToken returns the repository error
// unwrapped instead of mapping it to a generic sentinel — this test documents that actual
// behavior rather than asserting the (currently false) no-leak property the others have. It's
// still safe over the wire only because middleware.Auth's caller uses NewServerErrorResponse,
// which never puts err.Error() in the response body — but that safety net lives one layer away
// from this function, not in it.
func TestValidateTokenRepositoryErrorIsReturnedUnwrapped(t *testing.T) {
	underlying := errors.New("dial tcp 10.0.0.5:5432: connection refused")
	repo := &fakeSessionRepository{getByHashErr: underlying}
	svc := newTestService(repo)

	_, err := svc.ValidateToken(context.Background(), "raw-token")
	if !errors.Is(err, underlying) {
		t.Fatalf("err = %v, want the unwrapped repository error %v", err, underlying)
	}
}

func TestRevokeTokenByTokenHashSuccess(t *testing.T) {
	repo := &fakeSessionRepository{}
	svc := newTestService(repo)
	userID := uuid.New()

	if err := svc.RevokeTokenByTokenHash(context.Background(), userID, "hash"); err != nil {
		t.Fatalf("RevokeTokenByTokenHash: %v", err)
	}
	if len(repo.revokeCalls) != 1 || repo.revokeCalls[0] != (revokeCall{userID, "hash"}) {
		t.Fatalf("revokeCalls = %+v, want one call with (%v, hash)", repo.revokeCalls, userID)
	}
}

func TestRevokeTokenByTokenHashNotFound(t *testing.T) {
	repo := &fakeSessionRepository{revokeErr: repository.ErrSessionNotFound}
	svc := newTestService(repo)

	err := svc.RevokeTokenByTokenHash(context.Background(), uuid.New(), "hash")
	if !errors.Is(err, sessions.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestRevokeTokenByTokenHashRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeSessionRepository{revokeErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	err := svc.RevokeTokenByTokenHash(context.Background(), uuid.New(), "hash")
	if !errors.Is(err, sessions.ErrRevokeSession) {
		t.Fatalf("err = %v, want ErrRevokeSession", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestRevokeTokenHashesBeforeRevoking(t *testing.T) {
	repo := &fakeSessionRepository{}
	svc := newTestService(repo)
	userID := uuid.New()

	if err := svc.RevokeToken(context.Background(), userID, "raw-token"); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if len(repo.revokeCalls) != 1 || repo.revokeCalls[0].tokenHash != token.Hash("raw-token") {
		t.Fatalf("revokeCalls = %+v, want the hash of raw-token", repo.revokeCalls)
	}
}

func TestRefreshSessionSuccess(t *testing.T) {
	repo := &fakeSessionRepository{}
	svc := newTestService(repo)

	expiresAt, err := svc.RefreshSession(context.Background(), uuid.New(), "raw-token")
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expiresAt = %v, want a future time", expiresAt)
	}
}

func TestRefreshSessionNotFound(t *testing.T) {
	repo := &fakeSessionRepository{refreshErr: repository.ErrSessionNotFound}
	svc := newTestService(repo)

	_, err := svc.RefreshSession(context.Background(), uuid.New(), "raw-token")
	if !errors.Is(err, sessions.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestRefreshSessionRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeSessionRepository{refreshErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.RefreshSession(context.Background(), uuid.New(), "raw-token")
	if !errors.Is(err, sessions.ErrRefreshSession) {
		t.Fatalf("err = %v, want ErrRefreshSession", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestGetActiveSessionsMapsRepositoryRows(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expires := created.Add(7 * 24 * time.Hour)
	repo := &fakeSessionRepository{activeSessions: []repository.Session{
		{TokenHash: "hash", CreatedAt: created, ExpiresAt: expires, Origin: sessionscope.OriginLogin, Scope: sessionscope.ScopeWrite},
	}}
	svc := newTestService(repo)

	got, err := svc.GetActiveSessions(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetActiveSessions: %v", err)
	}
	if len(got) != 1 || got[0].TokenHash != "hash" || got[0].CreatedAt != created.Unix() || got[0].ExpiresAt != expires.Unix() {
		t.Fatalf("got = %+v, unexpected mapping", got)
	}
}

func TestGetActiveSessionsRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeSessionRepository{activeErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.GetActiveSessions(context.Background(), uuid.New())
	if !errors.Is(err, sessions.ErrGetActiveSessions) {
		t.Fatalf("err = %v, want ErrGetActiveSessions", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}
