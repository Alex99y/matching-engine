package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token has expired")
)

// Claims are the fields stored and signed inside the JWT.
// expiresOn from the original design is covered by RegisteredClaims.ExpiresAt,
// which the library validates automatically on Verify.
type Claims struct {
	jwt.RegisteredClaims
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
}

// Manager signs and verifies JWTs using HMAC-SHA256.
type Manager struct {
	secret []byte
}

func NewJWTManager(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

// Sign creates a signed JWT for the given user and session, valid for ttl duration.
func (m *Manager) Sign(userID, sessionID string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		UserID:    userID,
		SessionID: sessionID,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Verify parses and validates the token, returning its claims on success.
// Returns ErrExpiredToken if the token is valid but past its expiry,
// or ErrInvalidToken for any other failure (tampered, malformed, wrong algorithm).
func (m *Manager) Verify(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
