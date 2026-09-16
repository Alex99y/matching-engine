package deadletter

import (
	"encoding/json"
	"fmt"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/google/uuid"
)

// Verdict is what a command is: processable (Reason empty, Open or Cancel set) or dead, with the
// reason and the human-readable cause. Open is set for an invalid order too, so the matcher can
// still tell its owner.
type Verdict struct {
	Reason    Reason
	Error     string
	EventType string
	OrderID   uuid.UUID
	Open      *oeq.OpenOrderEvent
	Cancel    *oeq.CancelOrderEvent
}

func (v Verdict) Dead() bool { return v.Reason != "" }

// Classify is the one decision, shared by the matcher (on delivery) and the dead-letter consumer
// (on the parked copy), so the two can never disagree about why a command was refused. event is
// the parsed envelope, nil when the raw message did not parse.
func Classify(event *oeq.OrderEvent, constraints oeq.MarketConstraints) Verdict {
	if event == nil {
		return Verdict{Reason: ReasonMalformed, Error: "envelope did not parse"}
	}
	v := Verdict{EventType: string(event.Type)}

	switch event.Type {
	case oeq.EventTypeOpenOrder:
		open, err := event.DecodeOpenOrder()
		if err != nil {
			v.Reason, v.Error = ReasonMalformed, err.Error()
			return v
		}
		v.Open, v.OrderID = open, open.OrderID
		if err := oeq.ValidateOrderEvent(open, constraints); err != nil {
			v.Reason, v.Error = ReasonInvalid, err.Error()
		}
		return v

	case oeq.EventTypeCancelOrder:
		cancel, err := event.DecodeCancelOrder()
		if err != nil {
			v.Reason, v.Error = ReasonMalformed, err.Error()
			return v
		}
		v.Cancel, v.OrderID = cancel, cancel.OrderID
		return v

	default:
		v.Reason, v.Error = ReasonUnknownType, fmt.Sprintf("unknown event type %q", event.Type)
		return v
	}
}

func ClassifyRaw(raw []byte, constraints oeq.MarketConstraints) Verdict {
	event, err := oeq.ParseOrderEvent(raw)
	if err != nil {
		return Verdict{Reason: ReasonMalformed, Error: err.Error()}
	}
	return Classify(event, constraints)
}

// jsonPayload makes any message body storable in a JSONB column: bytes that are not valid JSON
// become a JSON string, so a malformed command is kept verbatim rather than refused twice.
func jsonPayload(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("null"), nil
	}
	if json.Valid(raw) {
		return json.RawMessage(raw), nil
	}
	quoted, err := json.Marshal(string(raw))
	if err != nil {
		return nil, fmt.Errorf("dead letter payload: %w", err)
	}
	return quoted, nil
}
