package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Generate returns a cryptographically random 32-byte token encoded as base64url
// (the raw token to hand to the client) and its SHA-256 hex digest (safe to store in the DB).
func Generate() (rawToken string, tokenHash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	return raw, Hash(raw), nil
}

// Hash returns the SHA-256 hex digest of rawToken. Use this to derive the stored hash
// from a client-supplied token before any DB lookup or comparison.
func Hash(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(h[:])
}
