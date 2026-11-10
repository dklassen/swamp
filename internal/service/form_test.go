package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

func TestFormService_Get(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")

	none, err := s.Forms.Get(ctx, p.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get before any save: err = %v, want store.ErrNotFound", err)
	}
	if none != nil {
		t.Errorf("Get before any save = %+v, want nil", none)
	}

	want := jobboard.ApplicationForm{
		Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required},
		Questions: []jobboard.Question{{Label: "Why us?", Required: true, Type: "long_text"}},
	}
	if err := s.Forms.Save(ctx, p.ID, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Forms.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(&want, got); diff != "" {
		t.Errorf("Get (-want +got):\n%s", diff)
	}
}

// A form saved by hand replaces whatever was stored, fetched or not.
func TestFormService_Save_ReplacesTheStoredForm(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")

	first := jobboard.ApplicationForm{Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required}}
	if err := s.Forms.Save(ctx, p.ID, first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	want := jobboard.ApplicationForm{
		Documents: map[document.Type]jobboard.Requirement{document.CoverLetter: jobboard.Optional},
		Questions: []jobboard.Question{{Label: "Why us?", Required: true, Type: "long_text"}},
	}
	if err := s.Forms.Save(ctx, p.ID, want); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	got, err := s.Forms.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(&want, got); diff != "" {
		t.Errorf("Get (-want +got):\n%s", diff)
	}
}
