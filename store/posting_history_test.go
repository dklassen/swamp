package store

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreatePostingHistory_ThenList_ReturnsCreatedEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	created, err := s.CreatePostingHistory(ctx, posting.ID, "content_updated", `{"title":"Software Engineer"}`)
	if err != nil {
		t.Fatalf("CreatePostingHistory: %v", err)
	}

	got, err := s.ListPostingHistory(ctx, posting.ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}

	want := []PostingHistory{created}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListPostingHistory mismatch (-want +got):\n%s", diff)
	}
}

func TestListPostingHistory_OnlyReturnsEntriesForThatPosting_InRecordedOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	other := mustUpsertPosting(t, s, acme.ID, "job-2", "Product Manager")

	first, err := s.CreatePostingHistory(ctx, posting.ID, "content_updated", `{"title":"Software Engineer"}`)
	if err != nil {
		t.Fatalf("CreatePostingHistory: %v", err)
	}
	second, err := s.CreatePostingHistory(ctx, posting.ID, "closed", `{"title":"Software Engineer"}`)
	if err != nil {
		t.Fatalf("CreatePostingHistory: %v", err)
	}
	if _, err := s.CreatePostingHistory(ctx, other.ID, "content_updated", `{"title":"Product Manager"}`); err != nil {
		t.Fatalf("CreatePostingHistory: %v", err)
	}

	got, err := s.ListPostingHistory(ctx, posting.ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}

	want := []PostingHistory{first, second}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListPostingHistory mismatch (-want +got):\n%s", diff)
	}
}
