package sync

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestSummarize covers the one-line summary `swamp fetch` prints and the
// TUI's sync-all shows (#151): companies, failures by name, and companies
// skipped because another sync of them was running (#150), mentioned only
// when there are any.
func TestSummarize(t *testing.T) {
	t.Parallel()
	fail := errors.New("status 404")
	tests := []struct {
		name        string
		results     []Result
		wantString  string
		wantFailed  []string
		wantSkipped []string
	}{
		{name: "none", results: nil, wantString: "0 companies, 0 failed"},
		{name: "one company, singular", results: []Result{{Name: "Acme"}}, wantString: "1 company, 0 failed"},
		{name: "one failure", results: []Result{{Name: "Acme"}, {Name: "Umbrella", Err: fail}},
			wantString: "2 companies, 1 failed (Umbrella)", wantFailed: []string{"Umbrella"}},
		{name: "failures in result order", results: []Result{{Name: "Globex", Err: fail}, {Name: "Acme"}, {Name: "Initech", Err: fail}},
			wantString: "3 companies, 2 failed (Globex, Initech)", wantFailed: []string{"Globex", "Initech"}},
		{name: "skipped is not failed", results: []Result{{Name: "Acme"}, {Name: "Globex", Err: ErrSyncInProgress}, {Name: "Initech", Err: fail}},
			wantString: "3 companies, 1 failed (Initech), 1 skipped (Globex)", wantFailed: []string{"Initech"}, wantSkipped: []string{"Globex"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Summarize(tt.results)
			if got.String() != tt.wantString {
				t.Errorf("String() = %q, want %q", got.String(), tt.wantString)
			}
			if diff := cmp.Diff(tt.wantFailed, got.Failed); diff != "" {
				t.Errorf("Failed mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSkipped, got.Skipped); diff != "" {
				t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
