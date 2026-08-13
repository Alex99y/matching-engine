package token_test

import (
	"encoding/base64"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/token"
)

func TestGenerate(t *testing.T) {
	raw, hash, err := token.Generate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw == "" {
		t.Fatal("expected a non-empty raw token")
	}
	if hash == "" {
		t.Fatal("expected a non-empty hash")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("raw token is not valid base64url: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("expected 32 raw bytes, got %d", len(decoded))
	}

	if hash != token.Hash(raw) {
		t.Error("Generate's hash must equal Hash(raw)")
	}
}

func TestGenerateUnique(t *testing.T) {
	raw1, hash1, err := token.Generate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw2, hash2, err := token.Generate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw1 == raw2 {
		t.Error("two consecutive calls must return different raw tokens")
	}
	if hash1 == hash2 {
		t.Error("two consecutive calls must return different hashes")
	}
}

func TestHash(t *testing.T) {
	// Known SHA-256 hex digest of the empty string.
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := token.Hash(""); got != want {
		t.Errorf("Hash(\"\") = %q, want %q", got, want)
	}
}

func TestHashDeterministic(t *testing.T) {
	if token.Hash("same-input") != token.Hash("same-input") {
		t.Error("Hash must be deterministic for the same input")
	}
	if token.Hash("a") == token.Hash("b") {
		t.Error("Hash of different inputs collided unexpectedly")
	}
}
