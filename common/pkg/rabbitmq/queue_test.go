// queue_test.go covers the pure, connection-independent pieces of Queue: message
// construction, the ConsumeArgs/MessageMetadata wrappers, and handleDelivery's
// dispatch to a delivery's Acknowledger (faked here), plus reopen's guards, which are
// reachable without a broker because they run before any channel is opened. Consume,
// consumeOnce, Publish and NewQueue itself drive a real *amqp091.Channel and need a live
// broker, so — like the rest of this package — they're left to integration testing.
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
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

func TestIsChannelClosed(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil is not a closed channel", nil, false},
		{"the sentinel itself", amqp091.ErrClosed, true},
		{"wrapped sentinel", fmt.Errorf("publish: %w", amqp091.ErrClosed), true},
		{"an unrelated amqp error", &amqp091.Error{Code: amqp091.NotFound, Reason: "no queue"}, false},
		{"a plain error", errors.New("connection reset by peer"), false},
		{"context cancellation", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isChannelClosed(tc.err); got != tc.want {
				t.Fatalf("isChannelClosed(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// A queue closed on purpose must not be resurrected by a later publish or by the consumer's
// retry loop — without the guard, reopen would hand back a live channel after Close and the
// queue would outlive its own shutdown.
func TestReopenRefusesAfterClose(t *testing.T) {
	q := &Queue{logger: logger.NewLogger(logger.Error), closed: true}

	if err := q.reopen(nil); !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("reopen after Close = %v, want ErrQueueClosed", err)
	}
}

// Concurrent publishers all observe the same dead channel. Only the first should replace it;
// the rest must see it already moved on and return without opening another.
func TestReopenIgnoresAStaleChannelThatWasAlreadyReplaced(t *testing.T) {
	current := &amqp091.Channel{}
	stale := &amqp091.Channel{}
	q := &Queue{logger: logger.NewLogger(logger.Error), channel: current}

	// The nil client would panic if reopen got as far as opening a channel, which is exactly
	// what must not happen here.
	if err := q.reopen(stale); err != nil {
		t.Fatalf("reopen with an already-replaced channel = %v, want nil", err)
	}
	if q.channel != current {
		t.Fatalf("reopen replaced a channel another caller had already refreshed")
	}
}
