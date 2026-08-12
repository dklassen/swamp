package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateTag_ThenGetByName_ReturnsSameTag(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.CreateTag(ctx, "remote")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	got, err := s.GetTagByName(ctx, "remote")
	if err != nil {
		t.Fatalf("GetTagByName: %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetTagByName mismatch (-created +got):\n%s", diff)
	}
}

func TestGetTagByName_NonexistentName_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetTagByName(ctx, "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetTagByName error = %v, want ErrNotFound", err)
	}
}

func TestListTags_ExcludesSoftDeletedTags(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	remote := mustCreateTag(t, s, "remote")
	staff := mustCreateTag(t, s, "staff-level")

	if err := s.SoftDeleteTag(ctx, staff.ID); err != nil {
		t.Fatalf("SoftDeleteTag: %v", err)
	}

	got, err := s.ListTags(ctx)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}

	want := []Tag{remote}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListTags mismatch (-want +got):\n%s", diff)
	}
}

func TestAddTagToPosting_ThenListTagsForPosting_ReturnsTag(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	remote := mustCreateTag(t, s, "remote")

	if err := s.AddTagToPosting(ctx, posting.ID, remote.ID); err != nil {
		t.Fatalf("AddTagToPosting: %v", err)
	}

	got, err := s.ListTagsForPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("ListTagsForPosting: %v", err)
	}

	want := []Tag{remote}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListTagsForPosting mismatch (-want +got):\n%s", diff)
	}
}

func TestListTagsForPosting_IncludesSoftDeletedTags(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	remote := mustCreateTag(t, s, "remote")

	if err := s.AddTagToPosting(ctx, posting.ID, remote.ID); err != nil {
		t.Fatalf("AddTagToPosting: %v", err)
	}
	if err := s.SoftDeleteTag(ctx, remote.ID); err != nil {
		t.Fatalf("SoftDeleteTag: %v", err)
	}

	got, err := s.ListTagsForPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("ListTagsForPosting: %v", err)
	}
	if len(got) != 1 || got[0].ID != remote.ID {
		t.Fatalf("ListTagsForPosting = %+v, want [remote] even though soft-deleted", got)
	}
}

func TestRemoveTagFromPosting_RemovesAssociation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	remote := mustCreateTag(t, s, "remote")

	if err := s.AddTagToPosting(ctx, posting.ID, remote.ID); err != nil {
		t.Fatalf("AddTagToPosting: %v", err)
	}
	if err := s.RemoveTagFromPosting(ctx, posting.ID, remote.ID); err != nil {
		t.Fatalf("RemoveTagFromPosting: %v", err)
	}

	got, err := s.ListTagsForPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("ListTagsForPosting: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListTagsForPosting after remove = %+v, want empty", got)
	}
}
