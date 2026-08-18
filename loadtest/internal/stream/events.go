package stream

import (
	"time"

	"github.com/google/uuid"
)

// OrderEvent is one order-state transition observed on the private user stream. Status mirrors
// the api's wire values: open | filled | partially_filled | cancelled | rejected. ReceivedAt is
// stamped with the local client clock the instant the frame is parsed — sender and listener run
// in the same process, so there's no cross-machine clock skew to correct for.
type OrderEvent struct {
	OrderID    uuid.UUID
	Status     string
	Filled     uint64
	Remaining  uint64
	ReceivedAt time.Time
}

const (
	StatusOpen            = "open"
	StatusFilled          = "filled"
	StatusPartiallyFilled = "partially_filled"
	StatusCancelled       = "cancelled"
	StatusRejected        = "rejected"
)

type wireOrderEvent struct {
	Type      string `json:"type"`
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Filled    string `json:"filled"`
	Remaining string `json:"remaining"`
}
