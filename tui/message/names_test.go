package message_test

import (
	"encoding/json"
	"fmt"
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

func TestEnums_EncodeAsTheirNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value fmt.Stringer
	}{
		{name: "kind", value: message.KindChanged},
		{name: "type", value: message.TypeCompany},
		{name: "op", value: message.OpDeleted},
		{name: "nav op", value: message.NavReplace},
		{name: "error code", value: message.ErrSyncInProgress},
		{name: "screen", value: message.ScreenCompanyDelete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal(%v): %v", tt.value, err)
			}
			want, err := json.Marshal(tt.value.String())
			if err != nil {
				t.Fatalf("Marshal(%q): %v", tt.value.String(), err)
			}
			if string(got) != string(want) {
				t.Errorf("Marshal(%v) = %s, want %s", tt.value, got, want)
			}
		})
	}
}

func decodeInto[E any](b []byte) error {
	var e E
	return json.Unmarshal(b, &e)
}
