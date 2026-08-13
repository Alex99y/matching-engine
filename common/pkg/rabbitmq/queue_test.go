// queue_test.go covers the pure, connection-independent pieces of Queue: message
// construction, the ConsumeArgs/MessageMetadata wrappers, and handleDelivery's
// dispatch to a delivery's Acknowledger (faked here). Consume/consumeOnce/reopen and
// NewQueue itself drive a real *amqp091.Channel and need a live broker, so — like the
// rest of this package — they're left to integration testing, not unit tests.
package rabbitmq

import (
	"errors"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	amqp091 "github.com/rabbitmq/amqp091-go"
)

func TestNewJSONPublishing(t *testing.T) {
	cases := []struct {
		name         string
		persistent   bool
		wantDelivery uint8
	}{
		{"transient", false, amqp091.Transient},
		{"persistent", true, amqp091.Persistent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pub := newJSONPublishing("msg-1", []byte(`{"a":1}`), c.persistent)
			if pub.ContentType != "application/json" {
				t.Errorf("ContentType = %q, want application/json", pub.ContentType)
			}
			if pub.MessageId != "msg-1" {
				t.Errorf("MessageId = %q, want msg-1", pub.MessageId)
			}
			if string(pub.Body) != `{"a":1}` {
				t.Errorf("Body = %q, want {\"a\":1}", pub.Body)
			}
			if pub.DeliveryMode != c.wantDelivery {
				t.Errorf("DeliveryMode = %v, want %v", pub.DeliveryMode, c.wantDelivery)
			}
		})
	}
}

func TestMessageMetadataGetters(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := MessageMetadata{
		messageType:     "order.created",
		messageEncoding: "utf-8",
		timestamp:       ts,
		expiration:      "60000",
	}

	if got := m.GetMsgType(); got != "order.created" {
		t.Errorf("GetMsgType() = %q, want %q", got, "order.created")
	}
	if got := m.GetMsgEncoding(); got != "utf-8" {
		t.Errorf("GetMsgEncoding() = %q, want %q", got, "utf-8")
	}
	if got := m.GetTimestamp(); !got.Equal(ts) {
		t.Errorf("GetTimestamp() = %v, want %v", got, ts)
	}
	if got := m.GetExpiration(); got != "60000" {
		t.Errorf("GetExpiration() = %q, want %q", got, "60000")
	}
}

func TestConsumeArgsGetters(t *testing.T) {
	var ackCalled, nackCalled, rejectCalled bool
	args := &ConsumeArgs{
		id:       "msg-1",
		message:  []byte("payload"),
		metadata: MessageMetadata{messageType: "t"},
		ack:      func() error { ackCalled = true; return nil },
		nack:     func() error { nackCalled = true; return errors.New("nack failed") },
		reject:   func() error { rejectCalled = true; return nil },
	}

	if got := args.Id(); got != "msg-1" {
		t.Errorf("Id() = %q, want msg-1", got)
	}
	if got := string(args.RawMessage()); got != "payload" {
		t.Errorf("RawMessage() = %q, want payload", got)
	}
	if got := args.GetMessageMetadata().GetMsgType(); got != "t" {
		t.Errorf("GetMessageMetadata().GetMsgType() = %q, want t", got)
	}

	if err := args.Ack(); err != nil || !ackCalled {
		t.Errorf("Ack() = %v, ackCalled=%v", err, ackCalled)
	}
	if err := args.Nack(); err == nil || !nackCalled {
		t.Errorf("Nack() = %v, nackCalled=%v, want an error and nackCalled=true", err, nackCalled)
	}
	if err := args.Reject(); err != nil || !rejectCalled {
		t.Errorf("Reject() = %v, rejectCalled=%v", err, rejectCalled)
	}
}

// fakeAcknowledger implements amqp091.Acknowledger without a real channel, so
// handleDelivery's Ack/Nack/Reject closures can be exercised end to end.
type fakeAcknowledger struct {
	ackTag, nackTag, rejectTag             uint64
	ackMultiple, nackMultiple, nackRequeue bool
	rejectRequeue                          bool
	ackCalled, nackCalled, rejectCalled    bool
}

func (f *fakeAcknowledger) Ack(tag uint64, multiple bool) error {
	f.ackCalled = true
	f.ackTag = tag
	f.ackMultiple = multiple
	return nil
}

func (f *fakeAcknowledger) Nack(tag uint64, multiple, requeue bool) error {
	f.nackCalled = true
	f.nackTag = tag
	f.nackMultiple = multiple
	f.nackRequeue = requeue
	return nil
}

func (f *fakeAcknowledger) Reject(tag uint64, requeue bool) error {
	f.rejectCalled = true
	f.rejectTag = tag
	f.rejectRequeue = requeue
	return nil
}

func TestQueueHandleDelivery(t *testing.T) {
	q := &Queue{logger: logger.NewLogger(logger.Error)}

	fake := &fakeAcknowledger{}
	delivery := amqp091.Delivery{
		Acknowledger:    fake,
		DeliveryTag:     7,
		MessageId:       "msg-1",
		Body:            []byte("payload"),
		Type:            "order.created",
		ContentEncoding: "utf-8",
		Expiration:      "60000",
	}

	var got *ConsumeArgs
	q.handleDelivery(delivery, func(a *ConsumeArgs) { got = a })

	if got == nil {
		t.Fatal("expected the callback to be invoked")
	}
	if got.Id() != "msg-1" {
		t.Errorf("Id() = %q, want msg-1", got.Id())
	}
	if string(got.RawMessage()) != "payload" {
		t.Errorf("RawMessage() = %q, want payload", got.RawMessage())
	}
	if got.GetMessageMetadata().GetMsgType() != "order.created" {
		t.Errorf("GetMsgType() = %q, want order.created", got.GetMessageMetadata().GetMsgType())
	}

	// handleDelivery wires ack -> Ack(false), nack -> Nack(false, true),
	// reject -> Reject(false) — see the closures in handleDelivery.
	if err := got.Ack(); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if !fake.ackCalled || fake.ackTag != 7 || fake.ackMultiple {
		t.Errorf("Ack delegation wrong: called=%v tag=%d multiple=%v", fake.ackCalled, fake.ackTag, fake.ackMultiple)
	}

	if err := got.Nack(); err != nil {
		t.Fatalf("Nack: %v", err)
	}
	if !fake.nackCalled || fake.nackTag != 7 || fake.nackMultiple || !fake.nackRequeue {
		t.Errorf("Nack delegation wrong: called=%v tag=%d multiple=%v requeue=%v",
			fake.nackCalled, fake.nackTag, fake.nackMultiple, fake.nackRequeue)
	}

	if err := got.Reject(); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if !fake.rejectCalled || fake.rejectTag != 7 || fake.rejectRequeue {
		t.Errorf("Reject delegation wrong: called=%v tag=%d requeue=%v",
			fake.rejectCalled, fake.rejectTag, fake.rejectRequeue)
	}
}
