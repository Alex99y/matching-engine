package stream

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Order-update statuses carried on the private stream. "rejected" and "expired" are
// stream-only distinctions — both persist as "cancelled" (see core's stream.go).
const (
	StatusOpen            = "open"
	StatusFilled          = "filled"
	StatusPartiallyFilled = "partially_filled"
	StatusCancelled       = "cancelled"
	StatusRejected        = "rejected"
	StatusExpired         = "expired"
)

type OrderEvent struct {
	OrderID   string
	Status    string
	Filled    uint64 // cumulative base filled (0 for a quote-denominated market buy)
	Remaining uint64
	At        time.Time // client receive time
}

// UserStream is a live subscriber to GET /stream/users for one account. It carries only that
// account's order updates — the broker routes private events by user id.
type UserStream struct{ *sub[OrderEvent] }

func ConnectUser(ctx context.Context, apiURL, token string) (*UserStream, error) {
	url := strings.TrimRight(apiURL, "/") + "/stream/users"

	s, err := subscribe(ctx, url, token, decodeOrder)
	if err != nil {
		return nil, err
	}
	return &UserStream{s}, nil
}

func decodeOrder(raw []byte) (OrderEvent, bool, error) {
	kind, err := frameType(raw)
	if err != nil {
		return OrderEvent{}, false, err
	}
	if kind != "order" {
		return OrderEvent{}, false, nil
	}

	var w struct {
		OrderID   string `json:"order_id"`
		Status    string `json:"status"`
		Filled    string `json:"filled"`
		Remaining string `json:"remaining"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return OrderEvent{}, false, err
	}
	return OrderEvent{
		OrderID:   w.OrderID,
		Status:    w.Status,
		Filled:    amount(w.Filled),
		Remaining: amount(w.Remaining),
		At:        time.Now(),
	}, true, nil
}

// WaitForStatus consumes events until orderID reaches one of statuses (returning that event)
// or ctx expires. Events for other orders, and other statuses for orderID, are skipped.
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

// Collect drains up to limit events for orderID until ctx expires, returning them in arrival
// order. Use it to assert on the whole lifecycle rather than a single transition.
func (s *UserStream) Collect(ctx context.Context, orderID string, limit int) []OrderEvent {
	var out []OrderEvent
	for len(out) < limit {
		ev, err := s.Next(ctx)
		if err != nil {
			return out
		}
		if ev.OrderID == orderID {
			out = append(out, ev)
		}
	}
	return out
}
