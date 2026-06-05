package jwt_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/api/pkg/jwt"
)

const (
	testSecret    = "test-secret-key"
	testUserID    = "550e8400-e29b-41d4-a716-446655440000"
	testSessionID = "session-abc-123"
)

func newManager() *jwt.JWTManager {
	return jwt.NewJWTManager(testSecret)
}

// --- Sign ---

func TestSign_ReturnsNonEmptyToken(t *testing.T) {
	token, err := newManager().Sign(testUserID, testSessionID, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestSign_TokenHasThreeParts(t *testing.T) {
	token, err := newManager().Sign(testUserID, testSessionID, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected JWT with 3 parts, got %d", len(parts))
	}
}

func TestSign_UniquePerCall(t *testing.T) {
	m := newManager()
	t1, _ := m.Sign(testUserID, testSessionID, time.Hour)
	t2, _ := m.Sign(testUserID, testSessionID, time.Hour)
	if t1 == t2 {
		t.Error("two tokens for the same user should differ (different IssuedAt)")
	}
}

// --- Verify ---

func TestVerify_ValidToken(t *testing.T) {
	m := newManager()
	token, err := m.Sign(testUserID, testSessionID, time.Hour)
	if err != nil {
		t.Fatalf("unexpected sign error: %v", err)
	}

	claims, err := m.Verify(token)
	if err != nil {
		t.Fatalf("unexpected verify error: %v", err)
	}
	if claims.UserID != testUserID {
		t.Errorf("expected UserID %q, got %q", testUserID, claims.UserID)
	}
	if claims.SessionID != testSessionID {
		t.Errorf("expected SessionID %q, got %q", testSessionID, claims.SessionID)
	}
	if claims.Subject != testUserID {
		t.Errorf("expected Subject %q, got %q", testUserID, claims.Subject)
	}
}

func TestVerify_ExpiredToken(t *testing.T) {
	m := newManager()
	token, err := m.Sign(testUserID, testSessionID, -time.Second)
	if err != nil {
		t.Fatalf("unexpected sign error: %v", err)
	}

	_, err = m.Verify(token)
	if !errors.Is(err, jwt.ErrExpiredToken) {
		t.Errorf("expected ErrExpiredToken, got %v", err)
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	token, err := newManager().Sign(testUserID, testSessionID, time.Hour)
	if err != nil {
		t.Fatalf("unexpected sign error: %v", err)
	}

	_, err = jwt.NewJWTManager("wrong-secret").Verify(token)
	if !errors.Is(err, jwt.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestVerify_MalformedToken(t *testing.T) {
	cases := []string{
		"",
		"not.a.jwt",
		"header.payload",
		"   ",
	}
	m := newManager()
	for _, tc := range cases {
		_, err := m.Verify(tc)
		if !errors.Is(err, jwt.ErrInvalidToken) {
			t.Errorf("input %q: expected ErrInvalidToken, got %v", tc, err)
		}
	}
}

func TestVerify_TamperedPayload(t *testing.T) {
	token, err := newManager().Sign(testUserID, testSessionID, time.Hour)
	if err != nil {
		t.Fatalf("unexpected sign error: %v", err)
	}

	parts := strings.Split(token, ".")
	parts[1] = parts[1] + "tampered"
	tampered := strings.Join(parts, ".")

	_, err = newManager().Verify(tampered)
	if !errors.Is(err, jwt.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for tampered payload, got %v", err)
	}
}

func TestVerify_TamperedSignature(t *testing.T) {
	token, err := newManager().Sign(testUserID, testSessionID, time.Hour)
	if err != nil {
		t.Fatalf("unexpected sign error: %v", err)
	}

	parts := strings.Split(token, ".")
	sig := []byte(parts[2])
	sig[0] ^= 0xFF
	parts[2] = string(sig)
	tampered := strings.Join(parts, ".")

	_, err = newManager().Verify(tampered)
	if !errors.Is(err, jwt.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for tampered signature, got %v", err)
	}
}

func TestVerify_AlgorithmNoneRejected(t *testing.T) {
	// "alg:none" attack: header.payload with empty signature
	// header: {"alg":"none","typ":"JWT"} — base64url encoded
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0" +
		".eyJ1c2VyX2lkIjoiYXR0YWNrZXIiLCJzZXNzaW9uX2lkIjoieHh4In0" +
		"."

	_, err := newManager().Verify(noneToken)
	if !errors.Is(err, jwt.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for alg=none token, got %v", err)
	}
}

func TestVerify_ClaimsRoundtrip(t *testing.T) {
	m := newManager()
	before := time.Now().Truncate(time.Second)

	token, err := m.Sign(testUserID, testSessionID, time.Hour)
	if err != nil {
		t.Fatalf("unexpected sign error: %v", err)
	}

	claims, err := m.Verify(token)
	if err != nil {
		t.Fatalf("unexpected verify error: %v", err)
	}

	if claims.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}
	if claims.IssuedAt == nil {
		t.Fatal("IssuedAt should not be nil")
	}
	if claims.IssuedAt.Before(before) {
		t.Errorf("IssuedAt %v is before test start %v", claims.IssuedAt.Time, before)
	}
	if !claims.ExpiresAt.After(claims.IssuedAt.Time) {
		t.Error("ExpiresAt should be after IssuedAt")
	}
}
