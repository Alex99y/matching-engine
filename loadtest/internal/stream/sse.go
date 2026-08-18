package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const reconnectBackoff = 500 * time.Millisecond

// UserStream is a long-lived subscriber to GET /stream/users for one authenticated account.
//
// The api's private stream carries NO replay on reconnect (docs/event-log.md): any order events
// published during a disconnect window are lost forever for this connection. UserStream
// auto-reconnects for resilience across long runs, but counts every reconnect via Reconnects()
// so callers can flag a run where a gap may have swallowed in-flight correlations rather than
// silently reporting those orders as "timed out" indistinguishably from real latency.
type UserStream struct {
	baseURL    string
	token      string
	httpClient *http.Client
	events     chan OrderEvent
	errs       chan error
	reconnects atomic.Int64
}

// Connect dials the stream once, synchronously, so the caller knows the listener is live before
// sending any order it intends to measure. Subsequent drops are retried in the background.
func Connect(ctx context.Context, baseURL, token string) (*UserStream, error) {
	s := &UserStream{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{}, // no Timeout: this is a deliberately long-lived connection
		events:     make(chan OrderEvent, 1024),
		errs:       make(chan error, 16),
	}

	resp, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	go s.run(ctx, resp)
	return s, nil
}

func (s *UserStream) Events() <-chan OrderEvent { return s.events }
func (s *UserStream) Errors() <-chan error      { return s.errs }
func (s *UserStream) Reconnects() int64         { return s.reconnects.Load() }

func (s *UserStream) dial(ctx context.Context) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/stream/users", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body) // best-effort diagnostic; a read failure here is reported as-is below
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

// run owns the reconnect loop. initial is the already-dialed connection from Connect so the very
// first frame isn't delayed by a redial.
func (s *UserStream) run(ctx context.Context, initial *http.Response) {
	defer close(s.events)
	resp := initial
	for {
		if resp == nil {
			var err error
			resp, err = s.dial(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				s.reconnects.Add(1)
				s.reportErr(err)
				if !s.wait(ctx) {
					return
				}
				continue
			}
		}

		err := s.consume(resp)
		resp.Body.Close()
		resp = nil
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.reportErr(err)
		}
		s.reconnects.Add(1)
		if !s.wait(ctx) {
			return
		}
	}
}

func (s *UserStream) wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(reconnectBackoff):
		return true
	}
}

// consume reads SSE frames until the connection ends. Only "data:" lines are assembled; ":"
// comment lines (the handler's keepalive ping) and any other SSE fields are ignored.
func (s *UserStream) consume(resp *http.Response) error {
	reader := bufio.NewReaderSize(resp.Body, 16*1024)
	var dataLines []string
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if len(dataLines) > 0 {
					s.handleFrame(strings.Join(dataLines, "\n"))
					dataLines = nil
				}
			case strings.HasPrefix(trimmed, "data:"):
				dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read stream: %w", err)
		}
	}
}

func (s *UserStream) handleFrame(raw string) {
	receivedAt := time.Now()

	var wire wireOrderEvent
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		s.reportErr(fmt.Errorf("decode frame: %w", err))
		return
	}
	if wire.Type != "order" {
		return
	}

	orderID, err := uuid.Parse(wire.OrderID)
	if err != nil {
		s.reportErr(fmt.Errorf("parse order_id %q: %w", wire.OrderID, err))
		return
	}
	filled, err := strconv.ParseUint(wire.Filled, 10, 64)
	if err != nil {
		s.reportErr(fmt.Errorf("parse filled %q: %w", wire.Filled, err))
		return
	}
	remaining, err := strconv.ParseUint(wire.Remaining, 10, 64)
	if err != nil {
		s.reportErr(fmt.Errorf("parse remaining %q: %w", wire.Remaining, err))
		return
	}

	event := OrderEvent{
		OrderID:    orderID,
		Status:     wire.Status,
		Filled:     filled,
		Remaining:  remaining,
		ReceivedAt: receivedAt,
	}
	select {
	case s.events <- event:
	default:
		// The buffer (1024) filling is not expected on a single-account private stream, but
		// dropping here beats blocking the reader goroutine and stalling the connection.
		s.reportErr(fmt.Errorf("events buffer full, dropped order %s status %s", orderID, wire.Status))
	}
}

func (s *UserStream) reportErr(err error) {
	select {
	case s.errs <- err:
	default:
	}
}
