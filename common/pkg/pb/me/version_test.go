package me_test

import (
	"errors"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/pb/me"
	mev1 "github.com/alex99y/matching-engine/common/pkg/pb/me/v1"
	"google.golang.org/protobuf/proto"
)

// Field 1 is the version in every envelope, so the peek must read it from any of them, whatever
// else the body carries, and must not be fooled by a body that has no such field.
func TestVersionReadsFieldOneOfAnyEnvelope(t *testing.T) {
	command, err := proto.Marshal(&mev1.OrderCommand{
		Version: 7,
		Command: &mev1.OrderCommand_Cancel{Cancel: &mev1.CancelOrder{MarketRef: "ETH-USDT"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := proto.Marshal(&mev1.Event{
		Version: 3, Epoch: "e1", Seq: 42, Market: "ETH-USDT",
		Payload: &mev1.Event_Heartbeat{Heartbeat: &mev1.Heartbeat{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	unversioned, err := proto.Marshal(&mev1.CancelOrder{MarketRef: "ETH-USDT"})
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name string
		raw  []byte
		want uint32
	}{
		{"order command", command, 7},
		{"event", event, 3},
		{"no field 1", unversioned, 0},
		{"empty", nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := me.Version(tt.raw)
			if err != nil {
				t.Fatalf("Version: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Version = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestVersionRejectsBytesThatAreNotProtobuf(t *testing.T) {
	for _, raw := range [][]byte{{0x80}, []byte("not protobuf")} {
		if _, err := me.Version(raw); !errors.Is(err, me.ErrNoVersion) {
			t.Fatalf("Version(%q) err = %v, want ErrNoVersion", raw, err)
		}
	}
}
