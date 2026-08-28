package stream

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Order-update statuses carried on the private stream. "rejected" and "expired" are
// stream-only distinctions — both persist as "cancelled" (see core stream.go).
const (
	StatusOpen            = "open"
	StatusFilled          = "filled"
	StatusPartiallyFilled = "partially_filled"
	StatusCancelled       = "cancelled"
	StatusRejected        = "rejected"
	StatusExpired         = "expired"
)

// ErrClosed is returned by Next/WaitForStatus after the stream has ended (server closed it,
// Close was called, or the connecting context was cancelled) with no other error to report.
var ErrClosed = errors.New("stream: closed")

type OrderEvent struct {
	OrderID   string
	Status    string
	Filled    uint64 // cumulative base filled (0 for a quote-denominated market buy)
	Remaining uint64
	At        time.Time // client receive time
}

type wireOrder struct {
	Type      string `json:"type"`
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Filled    string `json:"filled"`
	Remaining string `json:"remaining"`
}

// UserStream is a live subscriber to GET /stream/users for one account.
type UserStream struct {
	r       *reader
	events  chan OrderEvent
	errc    chan error
	stop    context.CancelFunc
	closeMu sync.Once
	closed  chan struct{}
}

// ConnectUser dials the stream synchronously (so a returned nil error means the listener is
// live) and starts decoding frames in the background.
func ConnectUser(ctx context.Context, apiURL, token string) (*UserStream, error) {
	sctx, cancel := context.WithCancel(ctx)
	url := strings.TrimRight(apiURL, "/") + "/stream/users"

	r, err := dial(sctx, url, token)
	if err != nil {
		cancel()
		return nil, err
	}

	s := &UserStream{
		r:      r,
		events: make(chan OrderEvent, 256),
		errc:   make(chan error, 1),
		stop:   cancel,
		closed: make(chan struct{}),
	}
	go s.pump()
	return s, nil
}

func (s *UserStream) pump() {
	defer close(s.events)
	defer s.stop() // release sctx if the stream ended on its own (server closed / parent ctx)
	for raw := range s.r.frames {
		var w wireOrder
		if err := json.Unmarshal(raw, &w); err != nil {
			s.report(err)
			continue
		}
		if w.Type != "order" {
			continue
		}
		filled, _ := strconv.ParseUint(w.Filled, 10, 64)
		remaining, _ := strconv.ParseUint(w.Remaining, 10, 64)
		ev := OrderEvent{OrderID: w.OrderID, Status: w.Status, Filled: filled, Remaining: remaining, At: time.Now()}
		select {
		case s.events <- ev:
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

// Next returns the next order event. It returns an error if the stream failed, ended
// (ErrClosed), or ctx expired.
func (s *UserStream) Next(ctx context.Context) (OrderEvent, error) {
	select {
	case err := <-s.errc:
		return OrderEvent{}, err
	case ev, ok := <-s.events:
		if !ok {
			return OrderEvent{}, ErrClosed
		}
		return ev, nil
	case <-ctx.Done():
		return OrderEvent{}, ctx.Err()
	}
}

// WaitForStatus consumes events until orderID reaches one of statuses (returning that event)
// or ctx expires. Events for other orders, and non-matching statuses for orderID, are skipped.
func (s *UserStream) WaitForStatus(ctx context.Context, orderID string, statuses ...string) (OrderEvent, error) {
	for {
		ev, err := s.Next(ctx)
		if err != nil {
			return OrderEvent{}, err
		}
		if ev.OrderID != orderID {
			continue
		}
		for _, want := range statuses {
			if ev.Status == want {
				return ev, nil
			}
		}
	}
}

// Close stops the stream. Safe to call more than once.
func (s *UserStream) Close() {
	s.closeMu.Do(func() {
		close(s.closed)
		s.stop()
	})
}

func (s *UserStream) report(err error) {
	select {
	case s.errc <- err:
	default:
	}
}
