package message_test

import (
	"encoding/json"
	"testing"

	"github.com/dklassen/swamp/tui/message"
)

func TestEnums_FailOnAValueOrNameWithoutAnEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		invalid any
		decode  func([]byte) error
	}{
		{name: "kind", invalid: message.Kind(0), decode: decodeInto[message.Kind]},
		{name: "type", invalid: message.Type(0), decode: decodeInto[message.Type]},
		{name: "op", invalid: message.Op(99), decode: decodeInto[message.Op]},
		{name: "nav op", invalid: message.NavOp(0), decode: decodeInto[message.NavOp]},
		{name: "error code", invalid: message.ErrorCode(0), decode: decodeInto[message.ErrorCode]},
		{name: "screen", invalid: message.Screen(99), decode: decodeInto[message.Screen]},
		{name: "listing status", invalid: message.ListingStatus(0), decode: decodeInto[message.ListingStatus]},
		{name: "application status", invalid: message.ApplicationStatus(0), decode: decodeInto[message.ApplicationStatus]},
		{name: "document type", invalid: message.DocumentType(0), decode: decodeInto[message.DocumentType]},
		{name: "review outcome", invalid: message.ReviewOutcome(0), decode: decodeInto[message.ReviewOutcome]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if b, err := json.Marshal(tt.invalid); err == nil {
				t.Errorf("Marshal(%v) = %s, want an error", tt.invalid, b)
			}
			if err := tt.decode([]byte(`"no_such_name"`)); err == nil {
				t.Errorf("Unmarshal of an unknown name: no error")
			}
		})
	}
}

func decodeInto[E any](b []byte) error {
	var e E
	return json.Unmarshal(b, &e)
}
