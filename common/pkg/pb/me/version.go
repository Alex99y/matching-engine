package me

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
)

var ErrNoVersion = errors.New("no schema version")

// Version reads the schema version of any envelope without decoding the rest of it. A body that
// carries no field 1 reports 0, which no codec accepts.
func Version(raw []byte) (uint32, error) {
	var h Header
	if err := (proto.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, &h); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrNoVersion, err)
	}
	return h.GetVersion(), nil
}
