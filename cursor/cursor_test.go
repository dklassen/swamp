package cursor

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestEncodeDecode_RoundTrips(t *testing.T) {
	t.Parallel()
	key := Key{Sort: "id_asc", Filters: Fingerprint(map[string]any{"CompanyID": 3}), ID: 812}

	got, err := Decode(Encode(key), key.Sort, key.Filters)
	if err != nil {
		t.Fatalf("Decode(Encode(key)): %v", err)
	}
	if diff := cmp.Diff(key, got); diff != "" {
		t.Errorf("round trip mismatch (-want +got):\n%s", diff)
	}
}

// TestEncode_IsOpaque: the token is URL-safe text a caller passes back
// unchanged, not a number it could adjust.
func TestEncode_IsOpaque(t *testing.T) {
	t.Parallel()
	token := Encode(Key{Sort: "id_asc", Filters: "f", ID: 812})
	if _, err := base64.RawURLEncoding.DecodeString(token); err != nil {
		t.Errorf("token %q isn't unpadded base64url: %v", token, err)
	}
	if token == "812" {
		t.Error("token is the bare ID")
	}
}

// TestDecode_TreatsTheTokenAsUntrusted: anything that isn't a token this
// package made for this search is rejected, never read as a position.
func TestDecode_TreatsTheTokenAsUntrusted(t *testing.T) {
	t.Parallel()
	filters := Fingerprint(map[string]any{"CompanyID": 3})
	raw := func(json string) string { return base64.RawURLEncoding.EncodeToString([]byte(json)) }

	tests := []struct {
		name    string
		token   string
		sort    string
		filters string
		want    error
	}{
		{"not base64", "not a cursor!", "id_asc", filters, ErrInvalid},
		{"not JSON", raw("812"), "id_asc", filters, ErrInvalid},
		{"unknown version", raw(`{"v":2,"sort":"id_asc","filters":"` + filters + `","id":812}`), "id_asc", filters, ErrInvalid},
		{"no version", raw(`{"sort":"id_asc","filters":"` + filters + `","id":812}`), "id_asc", filters, ErrInvalid},
		{"missing ID", raw(`{"v":1,"sort":"id_asc","filters":"` + filters + `"}`), "id_asc", filters, ErrInvalid},
		{"negative ID", raw(`{"v":1,"sort":"id_asc","filters":"` + filters + `","id":-5}`), "id_asc", filters, ErrInvalid},
		{"unknown field", raw(`{"v":1,"sort":"id_asc","filters":"` + filters + `","id":812,"offset":10}`), "id_asc", filters, ErrInvalid},
		{"another sort's cursor", Encode(Key{Sort: "id_desc", Filters: filters, ID: 812}), "id_asc", filters, ErrMismatch},
		{"another search's cursor", Encode(Key{Sort: "id_asc", Filters: Fingerprint(map[string]any{"CompanyID": 4}), ID: 812}), "id_asc", filters, ErrMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Decode(tt.token, tt.sort, tt.filters); !errors.Is(err, tt.want) {
				t.Errorf("Decode error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestFingerprint: equal filters give equal fingerprints whatever order
// their fields were built in; any changed value gives a different one.
func TestFingerprint(t *testing.T) {
	t.Parallel()
	a := Fingerprint(map[string]any{"CompanyID": 3, "Interested": true})
	b := Fingerprint(map[string]any{"Interested": true, "CompanyID": 3})
	c := Fingerprint(map[string]any{"CompanyID": 3, "Interested": false})
	if a != b {
		t.Errorf("same filters, different fingerprints: %q, %q", a, b)
	}
	if a == c {
		t.Errorf("different filters, same fingerprint %q", a)
	}
}
