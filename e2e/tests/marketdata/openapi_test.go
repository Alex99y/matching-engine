//go:build e2e

package marketdata

import (
	"strings"
	"testing"
)

// M6 — the served API description matches the API that is actually running.
//
// Fetches /openapi.yaml and checks it against the routes this suite exercises.
// Expect: it is an OpenAPI 3 document, and every path the suite depends on is described —
// a spec that has drifted from the code is worse than no spec, because clients generate
// against it.
func TestOpenAPISpecDescribesTheLiveRoutes(t *testing.T) {
	ctx := env.Context(t)

	raw, err := env.Client.OpenAPISpec(ctx)
	if err != nil {
		t.Fatalf("GET /openapi.yaml: %v", err)
	}
	spec := string(raw)

	if !strings.Contains(spec, "openapi: 3") {
		t.Fatalf("served document does not declare OpenAPI 3; first line: %q", firstLine(spec))
	}
	if !strings.Contains(spec, "paths:") {
		t.Fatal("served document has no paths section")
	}

	// Every route the suite actually calls must be described.
	paths := []string{
		"/users/register",
		"/users/check-username",
		"/users/balances",
		"/sessions",
		"/sessions/refresh",
		"/sessions/active",
		"/sessions/tokens",
		"/faucet",
		"/orders",
		"/instruments",
		"/markets",
		"/markets/{market}/depth",
		"/markets/{market}/matches",
		"/markets/{market}/candles",
		"/markets/prices",
		"/stream/users",
	}
	var missing []string
	for _, p := range paths {
		if !strings.Contains(spec, p+":") {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the spec omits routes the API serves: %s", strings.Join(missing, ", "))
	}

	// post_only is the newest field on the order-entry contract; its absence is the
	// signature of a spec that stopped being regenerated.
	if !strings.Contains(spec, "post_only") {
		t.Fatal("the spec has no post_only field — it has drifted from the order-entry contract")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
