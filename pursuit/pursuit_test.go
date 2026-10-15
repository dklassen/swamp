package pursuit_test

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/pursuit"
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

func newApplication(t *testing.T, s *store.Store) store.Application {
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
	app, err := s.CreateApplication(ctx, p.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	return app
}

func TestList_NoReviews_LatestReviewsEmpty(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	newApplication(t, s)

	got, err := pursuit.List(context.Background(), s)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].LatestReviews) != 0 {
		t.Fatalf("LatestReviews = %+v, want empty (no reviews recorded)", got[0].LatestReviews)
	}
}

func TestList_IncludesLatestDocumentReviews(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	app := newApplication(t, s)
	if _, err := s.CreateDocumentReview(ctx, app.ID, documents.CoverLetter, "draft one", store.ReviewOutcomeFlagged, "too generic"); err != nil {
		t.Fatalf("CreateDocumentReview (first): %v", err)
	}
	second, err := s.CreateDocumentReview(ctx, app.ID, documents.CoverLetter, "draft two", store.ReviewOutcomePassed, "")
	if err != nil {
		t.Fatalf("CreateDocumentReview (second): %v", err)
	}

	got, err := pursuit.List(ctx, s)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	review, ok := got[0].LatestReviews[documents.CoverLetter]
	if !ok {
		t.Fatalf("LatestReviews[%q] missing, want the most recent review present", documents.CoverLetter)
	}
	if review.ID != second.ID {
		t.Fatalf("LatestReviews[%q].ID = %d, want %d (most recent cycle, not the first)", documents.CoverLetter, review.ID, second.ID)
	}
	if _, ok := got[0].LatestReviews[documents.Resume]; ok {
		t.Fatalf("LatestReviews[%q] present, want absent (no resume review recorded)", documents.Resume)
	}
}
