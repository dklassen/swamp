package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestUpsertPosting_NewPosting_ThenGet_ReturnsSamePosting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	created, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID:  acme.ID,
		Source:     "ashby",
		SourceID:   "job-1",
		Title:      "Software Engineer",
		RawPayload: `{"id":"job-1"}`,
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	got, err := s.GetPosting(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetPosting mismatch (-created +got):\n%s", diff)
	}
}

func TestUpsertPosting_NewPosting_AutoCreatesMarkupRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	created, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID:  acme.ID,
		Source:     "ashby",
		SourceID:   "job-1",
		Title:      "Software Engineer",
		RawPayload: `{"id":"job-1"}`,
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	markup, err := s.GetPostingMarkup(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
	}

	want := PostingMarkup{
		PostingID:  created.ID,
		UserStatus: "new",
		Notes:      "",
		CreatedAt:  markup.CreatedAt,
		UpdatedAt:  markup.UpdatedAt,
	}
	if diff := cmp.Diff(want, markup); diff != "" {
		t.Fatalf("GetPostingMarkup mismatch (-want +got):\n%s", diff)
	}
}

func TestUpsertPosting_SameSourceAndSourceID_UpdatesInPlace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	first, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID:  acme.ID,
		Source:     "ashby",
		SourceID:   "job-1",
		Title:      "Software Engineer",
		RawPayload: `{"id":"job-1"}`,
	})
	if err != nil {
		t.Fatalf("UpsertPosting (first): %v", err)
	}

	second, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID:  acme.ID,
		Source:     "ashby",
		SourceID:   "job-1",
		Title:      "Senior Software Engineer",
		RawPayload: `{"id":"job-1","title":"Senior Software Engineer"}`,
	})
	if err != nil {
		t.Fatalf("UpsertPosting (second): %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("UpsertPosting (second).ID = %d, want %d (same posting)", second.ID, first.ID)
	}
	if second.Title != "Senior Software Engineer" {
		t.Fatalf("UpsertPosting (second).Title = %q, want %q", second.Title, "Senior Software Engineer")
	}

	all, err := s.ListPostingsByCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListPostingsByCompany = %d postings, want 1 (upsert should not duplicate)", len(all))
	}
}

func TestUpsertPosting_OnUpdate_DoesNotChangeListingStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}

	updated, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID:  acme.ID,
		Source:     "ashby",
		SourceID:   "job-1",
		Title:      "Software Engineer II",
		RawPayload: `{"id":"job-1"}`,
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	if updated.ListingStatus != "closed" {
		t.Fatalf("UpsertPosting.ListingStatus = %q, want %q (upsert must not reopen)", updated.ListingStatus, "closed")
	}
}

func TestGetPosting_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPosting(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPosting error = %v, want ErrNotFound", err)
	}
}

func TestGetPostingBySourceAndSourceID_ReturnsMatchingPosting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	created := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	got, err := s.GetPostingBySourceAndSourceID(ctx, "ashby", "job-1")
	if err != nil {
		t.Fatalf("GetPostingBySourceAndSourceID: %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetPostingBySourceAndSourceID mismatch (-created +got):\n%s", diff)
	}
}

func TestGetPostingBySourceAndSourceID_NoMatch_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPostingBySourceAndSourceID(ctx, "ashby", "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPostingBySourceAndSourceID error = %v, want ErrNotFound", err)
	}
}

func TestListPostingsByCompany_OnlyReturnsPostingsForThatCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")

	acmePosting := mustUpsertPosting(t, s, acme.ID, "job-1", "Acme Engineer")
	mustUpsertPosting(t, s, globex.ID, "job-2", "Globex Engineer")

	got, err := s.ListPostingsByCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}

	want := []Posting{acmePosting}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListPostingsByCompany mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkPostingClosed_SetsListingStatusClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}

	got, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if got.ListingStatus != "closed" {
		t.Fatalf("ListingStatus = %q, want %q", got.ListingStatus, "closed")
	}
}

func TestMarkPostingReopened_SetsListingStatusOpen(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if err := s.MarkPostingReopened(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingReopened: %v", err)
	}

	got, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if got.ListingStatus != "open" {
		t.Fatalf("ListingStatus = %q, want %q", got.ListingStatus, "open")
	}
}

func TestGetPostingMarkup_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPostingMarkup(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPostingMarkup error = %v, want ErrNotFound", err)
	}
}

func TestUpdatePostingMarkupStatus_UpdatesStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	updated, err := s.UpdatePostingMarkupStatus(ctx, posting.ID, "interested")
	if err != nil {
		t.Fatalf("UpdatePostingMarkupStatus: %v", err)
	}
	if updated.UserStatus != "interested" {
		t.Fatalf("UserStatus = %q, want %q", updated.UserStatus, "interested")
	}

	got, err := s.GetPostingMarkup(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
	}
	if diff := cmp.Diff(updated, got); diff != "" {
		t.Fatalf("GetPostingMarkup mismatch (-updated +got):\n%s", diff)
	}
}

func TestUpdatePostingMarkupNotes_UpdatesNotes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	updated, err := s.UpdatePostingMarkupNotes(ctx, posting.ID, "Looks like a great fit")
	if err != nil {
		t.Fatalf("UpdatePostingMarkupNotes: %v", err)
	}
	if updated.Notes != "Looks like a great fit" {
		t.Fatalf("Notes = %q, want %q", updated.Notes, "Looks like a great fit")
	}
}
