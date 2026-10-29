package tui

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/application"
)

func TestApplicationSnapshot_KeepsEveryStatus(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	for _, status := range application.All() {
		t.Run(status.String(), func(t *testing.T) {
			t.Parallel()
			want := application.Application{ID: 4, PostingID: 5, Status: status, Notes: "call Tuesday", CreatedAt: at, UpdatedAt: at, DeletedAt: at}

			s := applicationSnapshot(want)
			if s.Status.String() != status.String() {
				t.Errorf("snapshot status = %v, want %v", s.Status, status)
			}
			if diff := cmp.Diff(want, applicationOf(s)); diff != "" {
				t.Errorf("applicationOf(applicationSnapshot(a)) (-want +got):\n%s", diff)
			}
		})
	}
}
