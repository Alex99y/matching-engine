// Package stream consumes the API's Server-Sent Events endpoints. Each frame is a single
// `data: <json>\n\n` block; `:` comment frames (keepalive pings) are ignored. The private
// user stream carries no replay on reconnect, so callers connect before the action they
// intend to observe.
package stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StatusError is returned when an SSE endpoint responds with something other than 200.
type StatusError struct {
	URL    string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("stream %s: unexpected status %d: %s", e.URL, e.Status, e.Body)
}

// reader is a low-level SSE frame source: it dials once and pushes each frame's JSON payload
// onto frames until the connection ends or ctx is cancelled. A mid-stream read failure is
// delivered on errc (buffered, best-effort) just before frames closes.
type reader struct {
	frames chan []byte
	errc   chan error
}

func dial(ctx context.Context, url, token string) (*reader, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// No client timeout — the connection is deliberately long-lived; ctx cancels it.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &StatusError{URL: url, Status: resp.StatusCode, Body: string(body)}
	}

	r := &reader{frames: make(chan []byte, 256), errc: make(chan error, 1)}
	go r.consume(ctx, resp)
	return r, nil
}

func (r *reader) consume(ctx context.Context, resp *http.Response) {
	defer close(r.frames)
	defer resp.Body.Close()

	br := bufio.NewReaderSize(resp.Body, 32*1024)
	var data []string
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			switch t := strings.TrimRight(line, "\r\n"); {
			case t == "":
				if len(data) > 0 {
					select {
					case r.frames <- []byte(strings.Join(data, "\n")):
					case <-ctx.Done():
						return
					}
					data = nil
				}
			case strings.HasPrefix(t, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(t, "data:"), " "))
			}
		}
		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				select {
				case r.errc <- fmt.Errorf("read stream: %w", err):
				default:
				}
			}
			return
		}
	}
}
