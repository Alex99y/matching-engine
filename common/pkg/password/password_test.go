package password_test

import (
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/password"
)

func TestHash_ReturnsNonEmptyString(t *testing.T) {
	hash, err := password.Hash("secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestHash_PHCFormat(t *testing.T) {
	hash, err := password.Hash("secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Fatalf("expected 6 parts in PHC string, got %d: %q", len(parts), hash)
	}
	if parts[1] != "argon2id" {
		t.Errorf("expected algorithm argon2id, got %q", parts[1])
	}
	if !strings.HasPrefix(parts[2], "v=") {
		t.Errorf("expected version field, got %q", parts[2])
	}
}

func TestHash_UniquePerCall(t *testing.T) {
	h1, err := password.Hash("secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h2, err := password.Hash("secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h1 == h2 {
		t.Error("two hashes of the same password must differ (random salt)")
	}
}

func TestVerify_CorrectPassword(t *testing.T) {
	hash, err := password.Hash("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	match, err := password.Verify("correct-horse-battery-staple", hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Error("expected Verify to return true for the correct password")
	}
}

func TestVerify_WrongPassword(t *testing.T) {
	hash, err := password.Hash("correct")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	match, err := password.Verify("wrong", hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match {
		t.Error("expected Verify to return false for a wrong password")
	}
}

func TestVerify_EmptyPassword(t *testing.T) {
	hash, err := password.Hash("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	match, err := password.Verify("", hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Error("expected Verify to return true for empty password hashed as empty")
	}

	match, err = password.Verify("notempty", hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match {
		t.Error("expected Verify to return false when password does not match")
	}
}

func TestVerify_InvalidHashFormat(t *testing.T) {
	cases := []string{
		"",
		"notahash",
		"$argon2id$v=19$m=65536,t=3,p=4$onlyfourparts",
		"$bcrypt$v=19$m=65536,t=3,p=4$salt$hash",
	}

	for _, tc := range cases {
		match, err := password.Verify("password", tc)
		if err != password.ErrInvalidHash {
			t.Errorf("input %q: expected ErrInvalidHash, got err=%v match=%v", tc, err, match)
		}
	}
}

func TestVerify_IncompatibleVersion(t *testing.T) {
	// Manually craft a hash with a version that doesn't exist.
	tampered := "$argon2id$v=1$m=65536,t=3,p=4$c29tZXNhbHQ$c29tZWhhc2g"

	_, err := password.Verify("password", tampered)
	if err != password.ErrIncompatibleVersion {
		t.Errorf("expected ErrIncompatibleVersion, got %v", err)
	}
}

func TestVerify_TamperedHash(t *testing.T) {
	hash, err := password.Hash("original")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Flip the last character of the hash segment.
	parts := strings.Split(hash, "$")
	last := parts[5]
	if last[len(last)-1] == 'A' {
		parts[5] = last[:len(last)-1] + "B"
	} else {
		parts[5] = last[:len(last)-1] + "A"
	}
	tampered := strings.Join(parts, "$")

	match, err := password.Verify("original", tampered)
	if err != nil && err != password.ErrInvalidHash {
		t.Fatalf("unexpected error: %v", err)
	}
	if match {
		t.Error("expected Verify to return false for a tampered hash")
	}
}
