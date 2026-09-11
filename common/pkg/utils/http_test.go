package utils_test

import (
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/utils"
)

// A client reading an error must get something useful even when the reply did not come from one of
// our services — a proxy, or the wrong port — so the raw body is the fallback rather than "".
func TestAPIErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"our error shape", `{"message":"market not served by this core"}`, "market not served by this core"},
		{"other json without a message", `{"error":"boom"}`, `{"error":"boom"}`},
		{"empty message falls back to the body", `{"message":""}`, `{"message":""}`},
		{"not json at all", "  502 Bad Gateway\n", "502 Bad Gateway"},
		{"empty body", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := utils.APIErrorMessage([]byte(tt.body)); got != tt.want {
				t.Fatalf("APIErrorMessage(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}
