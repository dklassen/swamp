package postingmarkup_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/postingmarkup"
	"github.com/dklassen/swamp/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	sqlDB, err := store.Open(t.TempDir()+"/test.db", store.DefaultConfig())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return store.New(sqlDB)
}

func newPosting(t *testing.T, s *store.Store) int64 {
	t.Helper()
	ctx := context.Background()
	c, err := s.CreateCompany(ctx, "Acme", "ashby", "acme")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	p, err := s.UpsertPosting(ctx, store.CreatePostingParams{
		CompanyID: c.ID, Source: "ashby", SourceID: "job-1",
		IngestedFields: store.IngestedFields{Title: "Software Engineer", RawPayload: "{}"},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p.ID
}

func TestGet_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	_, err := postingmarkup.Get(context.Background(), s.DB(), 999)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get error = %v, want store.ErrNotFound", err)
	}
}

func TestUnmarkInterested_ClearsInterestedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	id := newPosting(t, s)
	if _, err := postingmarkup.SetInterested(ctx, s.DB(), id); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}

	updated, err := postingmarkup.UnmarkInterested(ctx, s.DB(), id)
	if err != nil {
		t.Fatalf("UnmarkInterested: %v", err)
	}
	if updated.InterestedAt != nil {
		t.Fatalf("InterestedAt = %v, want nil", updated.InterestedAt)
	}
}

func TestUnarchive_ClearsArchivedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	id := newPosting(t, s)
	if _, err := postingmarkup.SetArchived(ctx, s.DB(), id); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	updated, err := postingmarkup.Unarchive(ctx, s.DB(), id)
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if updated.ArchivedAt != nil {
		t.Fatalf("ArchivedAt = %v, want nil", updated.ArchivedAt)
	}
}

func TestUpdateNotes_UpdatesNotes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	id := newPosting(t, s)

	updated, err := postingmarkup.UpdateNotes(ctx, s.DB(), id, "Looks like a great fit")
	if err != nil {
		t.Fatalf("UpdateNotes: %v", err)
	}
	if updated.Notes != "Looks like a great fit" {
		t.Fatalf("Notes = %q, want %q", updated.Notes, "Looks like a great fit")
	}
}
