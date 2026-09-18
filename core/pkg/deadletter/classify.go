package deadletter

import (
	"encoding/json"

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
// the decoded body, nil when the raw message did not parse.
func Classify(event *oeq.OrderEvent, constraints oeq.MarketConstraints) Verdict {
	if event == nil {
		return Verdict{Reason: ReasonMalformed, Error: "body did not parse"}
	}
	v := Verdict{EventType: string(event.Type)}

	switch {
	case event.Open != nil:
		v.Open, v.OrderID = event.Open, event.Open.OrderID
		if err := oeq.ValidateOrderEvent(event.Open, constraints); err != nil {
			v.Reason, v.Error = ReasonInvalid, err.Error()
		}
	case event.Cancel != nil:
		v.Cancel, v.OrderID = event.Cancel, event.Cancel.OrderID
	default:
		v.Reason, v.Error = ReasonUnknownType, "no command this version knows"
	}
	return v
}

func ClassifyRaw(raw []byte, constraints oeq.MarketConstraints) Verdict {
	event, err := oeq.ParseOrderEvent(raw)
	if err != nil {
		return Verdict{Reason: ReasonMalformed, Error: err.Error()}
	}
	return Classify(event, constraints)
}

type undecodedPayload struct {
	Raw []byte `json:"raw_base64"`
}

// payloadJSON is what dead_letters.payload holds: the command rendered as JSON when the body
// decodes, otherwise the bytes themselves, base64-encoded so nothing is lost for a later replay.
func payloadJSON(raw []byte) json.RawMessage {
	if rendered, err := oeq.CommandJSON(raw); err == nil {
		return rendered
	}
	// A struct with one []byte field cannot fail to marshal.
	fallback, _ := json.Marshal(undecodedPayload{Raw: raw})
	return fallback
}
