package sync

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/ashby"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db")
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

func mustCreateCompany(t *testing.T, s *store.Store, name, source, sourceRef string) store.Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, source, sourceRef)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

// fakeFetcher is a PostingFetcher whose FetchPostings return value is
// configured per test, so sync tests never make real HTTP calls.
type fakeFetcher struct {
	postings map[string][]ashby.Posting // boardSlug -> postings
	err      error
}

func (f *fakeFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]ashby.Posting, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.postings[boardSlug], nil
}

// perBoardFetcher is a PostingFetcher that can fail for specific board
// slugs while succeeding for others, for testing SyncAll's per-company
// error isolation.
type perBoardFetcher struct {
	postings  map[string][]ashby.Posting
	errBoards map[string]error
}

func (f *perBoardFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]ashby.Posting, error) {
	if err, ok := f.errBoards[boardSlug]; ok {
		return nil, err
	}
	return f.postings[boardSlug], nil
}

func samplePosting(sourceID, title, department, location string) ashby.Posting {
	return ashby.Posting{
		SourceID:        sourceID,
		Title:           title,
		Department:      department,
		Location:        location,
		EmploymentType:  "FullTime",
		WorkplaceType:   "Remote",
		DescriptionHTML: "<p>desc</p>",
		DescriptionText: "desc",
		JobURL:          "https://jobs.ashbyhq.com/acme/" + sourceID,
		ApplicationURL:  "https://jobs.ashbyhq.com/acme/" + sourceID + "/application",
		PublishedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RawPayload:      []byte(`{"id":"` + sourceID + `"}`),
	}
}
