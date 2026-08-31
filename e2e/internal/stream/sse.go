// Package stream consumes the API's Server-Sent Events endpoints. Each frame is a single
// `data: <json>\n\n` block; `:` comment frames (keepalive pings) are ignored.
//
// None of these streams replay: a subscriber sees what happens after it connects (a market
// stream additionally opens with a snapshot of the current book). Tests therefore connect
// before the action they intend to observe.
package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// ErrClosed is returned by Next once the stream has ended — the server closed it, Close was
// called, or the connecting context was cancelled — with no other error to report.
var ErrClosed = errors.New("stream: closed")

// StatusError is returned when an SSE endpoint answers with something other than 200.
type StatusError struct {
	URL    string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("stream %s: unexpected status %d: %s", e.URL, e.Status, e.Body)
}

// reader is the low-level frame source: it dials once and pushes each frame's JSON payload
// onto frames until the connection ends or ctx is cancelled. A mid-stream read failure is
// delivered on errc (buffered, best effort) just before frames closes.
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

// decoder turns one frame into an event. keep reports whether the event is of interest —
// frame types a particular subscription does not care about are dropped rather than surfaced.
type decoder[T any] func(raw []byte) (event T, keep bool, err error)

// sub is the shared lifecycle behind every typed stream: decode frames in the background,
// hand them to Next one at a time, and shut down exactly once.
type sub[T any] struct {
	r      *reader
	events chan T
	errc   chan error
	stop   context.CancelFunc
	once   sync.Once
	closed chan struct{}
}

func subscribe[T any](ctx context.Context, url, token string, decode decoder[T]) (*sub[T], error) {
	sctx, cancel := context.WithCancel(ctx)

	r, err := dial(sctx, url, token)
	if err != nil {
		cancel()
		return nil, err
	}

	s := &sub[T]{
		r:      r,
		events: make(chan T, 256),
		errc:   make(chan error, 1),
		stop:   cancel,
		closed: make(chan struct{}),
	}
	go s.pump(decode)
	return s, nil
}

func (s *sub[T]) pump(decode decoder[T]) {
	defer close(s.events)
	defer s.stop() // release the context if the stream ended on its own

	for raw := range s.r.frames {
		event, keep, err := decode(raw)
		if err != nil {
			s.report(err)
			continue
		}
		if !keep {
			continue
		}
		select {
		case s.events <- event:
		case <-s.closed:
			return
		}
	}
	// frames closed — surface a final read error if consume left one.
	select {
	case err := <-s.r.errc:
		s.report(err)
	default:
	}
}

// Next returns the next event, or an error if the stream failed, ended (ErrClosed), or ctx
// expired.
func (s *sub[T]) Next(ctx context.Context) (T, error) {
	var zero T
	select {
	case err := <-s.errc:
		return zero, err
	case event, ok := <-s.events:
		if !ok {
			return zero, ErrClosed
		}
		return event, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// Close stops the stream. Safe to call more than once.
func (s *sub[T]) Close() {
	s.once.Do(func() {
		close(s.closed)
		s.stop()
	})
}

func (s *sub[T]) report(err error) {
	select {
	case s.errc <- err:
	default:
	}
}

// frameType reads just the discriminator so a decoder can dispatch before unmarshalling the
// whole payload.
func frameType(raw []byte) (string, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", fmt.Errorf("decode frame: %w", err)
	}
	return probe.Type, nil
}

// amount parses one of the decimal-string amounts the API sends in place of JSON numbers
// (a uint64 quantity would lose precision as a JavaScript number).
func amount(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}
