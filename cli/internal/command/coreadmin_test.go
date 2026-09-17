package command

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type capturedRequest struct {
	method  string
	path    string
	rawPath string
	query   string
	auth    string
	body    string
}

// adminStub stands in for core's admin API, recording what the CLI sent and replying with whatever
// the test needs. Every command here reaches core over HTTP rather than the database, so the request
// itself is the thing worth asserting.
type adminStub struct {
	*httptest.Server
	got    []capturedRequest
	status int
	body   string
}

func newAdminStub(t *testing.T, body string) *adminStub {
	t.Helper()
	stub := &adminStub{status: http.StatusOK, body: body}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		stub.got = append(stub.got, capturedRequest{
			method:  r.Method,
			path:    r.URL.Path,
			rawPath: r.URL.EscapedPath(),
			query:   r.URL.RawQuery,
			auth:    r.Header.Get("Authorization"),
			body:    string(raw),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stub.status)
		io.WriteString(w, stub.body)
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (s *adminStub) last(t *testing.T) capturedRequest {
	t.Helper()
	if len(s.got) == 0 {
		t.Fatal("no request reached the admin API")
	}
	return s.got[len(s.got)-1]
}

func (s *adminStub) client(t *testing.T) *coreAdminClient {
	t.Helper()
	c, err := newCoreAdminClient(s.URL, "secret-token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return c
}

// The token is the only thing standing between a scrapeable network and a trading halt, so every
// request must carry it.
func TestEveryRequestCarriesTheBearerToken(t *testing.T) {
	calls := map[string]struct {
		reply string
		call  func(*coreAdminClient) error
	}{
		"pause":         {`{}`, func(c *coreAdminClient) error { _, err := c.setPaused("ETH-USDT", true); return err }},
		"resume":        {`{}`, func(c *coreAdminClient) error { _, err := c.setPaused("ETH-USDT", false); return err }},
		"status":        {`[]`, func(c *coreAdminClient) error { _, err := c.status(); return err }},
		"user orders":   {`{}`, func(c *coreAdminClient) error { _, err := c.userOrders("alice", false); return err }},
		"cancel orders": {`{}`, func(c *coreAdminClient) error { _, err := c.cancelUserOrders("alice", nil, true); return err }},
	}

	for name, tt := range calls {
		t.Run(name, func(t *testing.T) {
			stub := newAdminStub(t, tt.reply)
			if err := tt.call(stub.client(t)); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got := stub.last(t).auth; got != "Bearer secret-token" {
				t.Fatalf("Authorization = %q, want %q", got, "Bearer secret-token")
			}
		})
	}
}

// Pause and resume differ only by the last path segment. Getting them the wrong way round would halt
// a market an operator meant to restart.
func TestSetPausedTargetsTheRightRoute(t *testing.T) {
	tests := []struct {
		name   string
		paused bool
		want   string
	}{
		{"pause", true, "/admin/markets/ETH-USDT/pause"},
		{"resume", false, "/admin/markets/ETH-USDT/resume"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newAdminStub(t, `{"market":"ETH-USDT","paused":true}`)
			if _, err := stub.client(t).setPaused("ETH-USDT", tt.paused); err != nil {
				t.Fatal(err)
			}

			got := stub.last(t)
			if got.path != tt.want {
				t.Fatalf("path = %q, want %q", got.path, tt.want)
			}
			if got.method != http.MethodPost {
				t.Fatalf("method = %q, want POST", got.method)
			}
			// A GET-shaped request carries no body, so no Content-Type is set either.
			if got.body != "" {
				t.Fatalf("body = %q, want empty", got.body)
			}
		})
	}
}

// A market ref is user input landing in a URL path. Escaping it keeps a stray slash from adding path
// segments and addressing a different route — the escaped form is what goes on the wire, even though
// the server decodes it back before routing.
func TestMarketRefIsPathEscaped(t *testing.T) {
	stub := newAdminStub(t, `{}`)
	if _, err := stub.client(t).setPaused("ETH/USDT", true); err != nil {
		t.Fatal(err)
	}
	if got := stub.last(t).rawPath; got != "/admin/markets/ETH%2FUSDT/pause" {
		t.Fatalf("escaped path = %q, want the ref escaped", got)
	}
}

func TestUserOrdersRequestsCancelledOnlyWhenAsked(t *testing.T) {
	tests := []struct {
		name             string
		includeCancelled bool
		wantQuery        string
	}{
		{"open only", false, ""},
		{"including cancelled", true, "cancelled=true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newAdminStub(t, `{"username":"alice","frozen":true,"orders":[]}`)
			if _, err := stub.client(t).userOrders("alice", tt.includeCancelled); err != nil {
				t.Fatal(err)
			}

			got := stub.last(t)
			if got.path != "/admin/users/alice/orders" {
				t.Fatalf("path = %q", got.path)
			}
			if got.query != tt.wantQuery {
				t.Fatalf("query = %q, want %q", got.query, tt.wantQuery)
			}
			if got.method != http.MethodGet {
				t.Fatalf("method = %q, want GET", got.method)
			}
		})
	}
}

// "all" and an explicit id list are different requests to the server, and sending both — or
// defaulting all to true — would cancel far more than the operator asked for.
func TestCancelBodyDistinguishesAllFromExplicitIDs(t *testing.T) {
	tests := []struct {
		name     string
		orderIDs []string
		all      bool
		wantAll  bool
		wantIDs  []any
	}{
		{"every open order", nil, true, true, nil},
		{"explicit ids", []string{"id-1", "id-2"}, false, false, []any{"id-1", "id-2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newAdminStub(t, `{"username":"alice","queued":0,"skipped":0,"results":[]}`)
			if _, err := stub.client(t).cancelUserOrders("alice", tt.orderIDs, tt.all); err != nil {
				t.Fatal(err)
			}

			got := stub.last(t)
			if got.path != "/admin/users/alice/orders/cancel" || got.method != http.MethodPost {
				t.Fatalf("%s %s, want POST /admin/users/alice/orders/cancel", got.method, got.path)
			}

			var body map[string]any
			if err := json.Unmarshal([]byte(got.body), &body); err != nil {
				t.Fatalf("body %q: %v", got.body, err)
			}
			if body["all"] != tt.wantAll {
				t.Fatalf("all = %v, want %v", body["all"], tt.wantAll)
			}
			if tt.wantIDs == nil {
				if _, present := body["order_ids"]; present {
					t.Fatalf("order_ids should be absent, got %v", body["order_ids"])
				}
				return
			}
			ids, ok := body["order_ids"].([]any)
			if !ok || len(ids) != len(tt.wantIDs) {
				t.Fatalf("order_ids = %v, want %v", body["order_ids"], tt.wantIDs)
			}
			for i := range ids {
				if ids[i] != tt.wantIDs[i] {
					t.Fatalf("order_ids[%d] = %v, want %v", i, ids[i], tt.wantIDs[i])
				}
			}
		})
	}
}

// The API's refusals are the operator's instructions — "freeze the account first", "no such user".
// Swallowing the body would reduce a 409 that says exactly what to do to a bare status line.
func TestServerErrorsSurfaceTheAPIMessage(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"conflict", http.StatusConflict, `{"message":"user must be frozen before their orders can be cancelled"}`,
			"user must be frozen"},
		{"not found", http.StatusNotFound, `{"message":"user not found"}`, "user not found"},
		{"unauthorized", http.StatusUnauthorized, `{"message":"invalid token"}`, "invalid token"},
		// Not one of our services answering — a proxy, or the metrics port by mistake.
		{"non-json body", http.StatusBadGateway, "upstream connect error", "upstream connect error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newAdminStub(t, tt.body)
			stub.status = tt.status

			_, err := stub.client(t).cancelUserOrders("alice", nil, true)
			if err == nil {
				t.Fatal("a non-200 must be an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// An unreachable core is the common case when CORE_ADMIN_URL points at the wrong port. The error has
// to name the URL that was tried.
func TestAnUnreachableCoreNamesTheURL(t *testing.T) {
	stub := newAdminStub(t, `{}`)
	client := stub.client(t)
	stub.Close()

	_, err := client.status()
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if !strings.Contains(err.Error(), stub.URL) {
		t.Fatalf("err = %q, want it to name %q", err, stub.URL)
	}
}

func TestNewCoreAdminClientRequiresURLAndToken(t *testing.T) {
	t.Setenv(coreAdminURLEnv, "")
	t.Setenv(coreAdminTokenEnv, "")

	if _, err := newCoreAdminClient("", "token"); err != errAdminURLRequired {
		t.Fatalf("err = %v, want errAdminURLRequired", err)
	}
	if _, err := newCoreAdminClient("http://localhost:9000", ""); err != errAdminTokenRequired {
		t.Fatalf("err = %v, want errAdminTokenRequired", err)
	}
	if _, err := newCoreAdminClient("   ", "token"); err != errAdminURLRequired {
		t.Fatalf("whitespace URL accepted: %v", err)
	}
}

func TestNewCoreAdminClientFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv(coreAdminURLEnv, "http://core.internal:9000/")
	t.Setenv(coreAdminTokenEnv, "env-token")

	c, err := newCoreAdminClient("", "")
	if err != nil {
		t.Fatal(err)
	}
	// The trailing slash is trimmed, or every path would be built with a double slash.
	if c.baseURL != "http://core.internal:9000" {
		t.Fatalf("baseURL = %q", c.baseURL)
	}
	if c.token != "env-token" {
		t.Fatalf("token = %q", c.token)
	}
}

func TestExplicitFlagsWinOverTheEnvironment(t *testing.T) {
	t.Setenv(coreAdminURLEnv, "http://wrong:1111")
	t.Setenv(coreAdminTokenEnv, "wrong-token")

	c, err := newCoreAdminClient("http://right:9000", "right-token")
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "http://right:9000" || c.token != "right-token" {
		t.Fatalf("flags did not win: %q %q", c.baseURL, c.token)
	}
}

// Targeting is validated before the client is built, so a mistake costs nothing and, critically,
// "cancel-orders" with no target never reaches the server as a cancel-everything.
func TestCancelOrdersRejectsAmbiguousTargeting(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want error
	}{
		{"no target", []string{"--username", "alice"}, errCancelTargetRequired},
		{"both targets", []string{"--username", "alice", "--all", "--order-id", "id-1"}, errCancelTargetAmbiguous},
		{"no username", []string{"--all"}, errUserBalanceUsernameRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newAdminStub(t, `{}`)
			args := append(tt.args, "--core-url", stub.URL, "--token", "t")

			if _, err := runCommand(t, newUserCancelOrdersCmd(), args...); err != tt.want {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if len(stub.got) != 0 {
				t.Fatal("a rejected command must not reach the server")
			}
		})
	}
}

func TestMarketPauseRejectsABadMarketRef(t *testing.T) {
	tests := []struct {
		name   string
		market string
		want   string
	}{
		{"missing", "", "--market is required"},
		{"not a pair", "NOTAMARKET", "invalid market"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newAdminStub(t, `{}`)
			args := []string{"--core-url", stub.URL, "--token", "t"}
			if tt.market != "" {
				args = append(args, "--market", tt.market)
			}

			_, err := runCommand(t, newMarketPauseCmd(), args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
			if len(stub.got) != 0 {
				t.Fatal("a rejected command must not reach the server")
			}
		})
	}
}

func TestMarketPauseReportsTheStateCoreReturned(t *testing.T) {
	stub := newAdminStub(t, `{"market":"ETH-USDT","paused":true}`)

	out, err := runCommand(t, newMarketPauseCmd(),
		"--market", "ETH-USDT", "--core-url", stub.URL, "--token", "t")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "market ETH-USDT paused") {
		t.Fatalf("output = %q", out)
	}
}

func TestMarketStatusRendersEveryMarket(t *testing.T) {
	stub := newAdminStub(t, `[{"market":"BTC-USDT","paused":false},{"market":"ETH-USDT","paused":true}]`)

	out, err := runCommand(t, newMarketStatusCmd(), "--core-url", stub.URL, "--token", "t")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BTC-USDT", "trading", "ETH-USDT", "paused"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestMarketStatusSaysSoWhenCoreServesNothing(t *testing.T) {
	stub := newAdminStub(t, `[]`)

	out, err := runCommand(t, newMarketStatusCmd(), "--core-url", stub.URL, "--token", "t")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no markets") {
		t.Fatalf("output = %q", out)
	}
}

// An operator reads this table to decide whether a cancel will land, so an unreachable order has to
// show its reason rather than looking like any other open order.
func TestUserOrdersRendersReachability(t *testing.T) {
	stub := newAdminStub(t, `{
		"username":"alice","frozen":true,
		"orders":[
			{"order_id":"o-1","market":"ETH-USDT","side":"buy","price":100,"remaining":5,"open":true,"reachable":true},
			{"order_id":"o-2","market":"BTC-USDT","side":"sell","open":true,"reachable":false,"unreachable_reason":"market is paused"},
			{"order_id":"o-3","open":false,"reachable":false}
		]}`)

	out, err := runCommand(t, newUserOrdersCmd(),
		"--username", "alice", "--core-url", stub.URL, "--token", "t")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"frozen: true",
		"o-1", "ETH-USDT", "100",
		"o-2", "open (market is paused)",
		"o-3", "closed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

// A closed order carries no market, side, price or remaining quantity. Those have to render as a
// placeholder rather than as an empty column or a zero that reads like a real price.
func TestUserOrdersRendersAbsentFieldsAsPlaceholders(t *testing.T) {
	stub := newAdminStub(t, `{"username":"alice","frozen":false,
		"orders":[{"order_id":"o-1","open":false,"reachable":false}]}`)

	out, err := runCommand(t, newUserOrdersCmd(),
		"--username", "alice", "--core-url", stub.URL, "--token", "t")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, " 0 ") {
		t.Fatalf("an absent price rendered as 0: %q", out)
	}
	if !strings.Contains(out, "-") {
		t.Fatalf("output %q has no placeholder for the absent fields", out)
	}
}

func TestUserOrdersSaysSoWhenThereAreNone(t *testing.T) {
	stub := newAdminStub(t, `{"username":"alice","frozen":false,"orders":[]}`)

	out, err := runCommand(t, newUserOrdersCmd(),
		"--username", "alice", "--core-url", stub.URL, "--token", "t")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no orders") {
		t.Fatalf("output = %q", out)
	}
}

// A skipped order is still live. The summary has to say so plainly, because the operator's next move
// is to fix the reason and re-run rather than assume the account is clean.
func TestCancelOrdersReportsSkippedOrdersAsStillLive(t *testing.T) {
	stub := newAdminStub(t, `{
		"username":"alice","queued":1,"skipped":1,
		"results":[
			{"order_id":"o-1","market":"ETH-USDT","queued":true},
			{"order_id":"o-2","market":"BTC-USDT","queued":false,"reason":"market is paused"}
		]}`)

	out, err := runCommand(t, newUserCancelOrdersCmd(),
		"--username", "alice", "--all", "--core-url", stub.URL, "--token", "t")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"o-1", "queued",
		"o-2", "SKIPPED: market is paused",
		"1 cancel(s) queued, 1 skipped",
		"still live",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestCancelOrdersOmitsTheWarningWhenNothingWasSkipped(t *testing.T) {
	stub := newAdminStub(t, `{"username":"alice","queued":1,"skipped":0,
		"results":[{"order_id":"o-1","market":"ETH-USDT","queued":true}]}`)

	out, err := runCommand(t, newUserCancelOrdersCmd(),
		"--username", "alice", "--all", "--core-url", stub.URL, "--token", "t")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "still live") {
		t.Fatalf("warned about skipped orders when there were none: %q", out)
	}
}

// These commands reach core over HTTP, so rootCmd must not try to open a database connection for
// them — without the annotation the CLI would fail before it ever sent the request.
func TestCoreAdminCommandsSkipTheDatabase(t *testing.T) {
	commands := map[string]func() *cobra.Command{
		"market pause":  newMarketPauseCmd,
		"market resume": newMarketResumeCmd,
		"market status": newMarketStatusCmd,
		"user orders":   newUserOrdersCmd,
		"cancel-orders": newUserCancelOrdersCmd,
	}

	for name, build := range commands {
		t.Run(name, func(t *testing.T) {
			if build().Annotations[annotationSkipDB] != "true" {
				t.Fatalf("%s must be annotated %q", name, annotationSkipDB)
			}
		})
	}
}

func TestOrderStateNamesEachCase(t *testing.T) {
	tests := []struct {
		name  string
		order userOrder
		want  string
	}{
		{"closed", userOrder{Open: false}, "closed"},
		{"open and reachable", userOrder{Open: true, Reachable: true}, "open"},
		{"open but unreachable", userOrder{Open: true, Unreachable: "market is paused"}, "open (market is paused)"},
		// A closed order is closed regardless of whether core could reach its market.
		{"closed wins over reachability", userOrder{Open: false, Reachable: true}, "closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orderState(tt.order); got != tt.want {
				t.Fatalf("orderState = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPausedWord(t *testing.T) {
	if got := pausedWord(true); got != "paused" {
		t.Fatalf("pausedWord(true) = %q", got)
	}
	if got := pausedWord(false); got != "trading" {
		t.Fatalf("pausedWord(false) = %q", got)
	}
}

// The listing defaults to every market and lets the server pick the page size; only what the
// operator asked for reaches the query string, so the server's defaults stay the single source.
func TestDeadLettersQueryCarriesOnlyWhatWasAsked(t *testing.T) {
	tests := []struct {
		name      string
		market    string
		limit     int
		wantQuery string
	}{
		{"defaults", "", 0, ""},
		{"market only", "ETH-USDT", 0, "market=ETH-USDT"},
		{"market and limit", "ETH-USDT", 20, "limit=20&market=ETH-USDT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newAdminStub(t, `{"items":[{"id":1,"market":"ETH-USDT","reason":"invalid","dead_at":1,"recorded_at":1,"status":"parked","payload":{}}]}`)
			out, err := stub.client(t).deadLetters(tt.market, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			got := stub.last(t)
			if got.method != http.MethodGet || got.path != "/admin/dlq" {
				t.Fatalf("request = %s %s, want GET /admin/dlq", got.method, got.path)
			}
			if got.query != tt.wantQuery {
				t.Fatalf("query = %q, want %q", got.query, tt.wantQuery)
			}
			if len(out.Items) != 1 || out.Items[0].Reason != "invalid" || string(out.Items[0].Payload) != "{}" {
				t.Fatalf("items = %+v, want the one record with its payload", out.Items)
			}
		})
	}
}
