package tui

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
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

// mustUpsertPosting creates a posting directly via the store, bypassing
// the syncer -- for tests that need a posting/application to exist
// without driving a full sync.
func mustUpsertPosting(t *testing.T, s *store.Store, companyID int64, sourceID, title string) store.Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{
		CompanyID:      companyID,
		Source:         "ashby",
		SourceID:       sourceID,
		IngestedFields: store.IngestedFields{Title: title},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

func mustCreateApplication(t *testing.T, s *store.Store, postingID int64) store.Application {
	t.Helper()
	a, err := s.CreateApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	return a
}

// fakeFetcher is a sync.PostingFetcher whose FetchPostings return value is
// configured per test, so tui tests never make real HTTP calls.
type fakeFetcher struct {
	postings map[string][]jobboard.Posting
}

func (f *fakeFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	return f.postings[boardSlug], nil
}

func newTestSyncer(s *store.Store, postings map[string][]jobboard.Posting) *sync.Syncer {
	return sync.New(s, map[string]sync.PostingFetcher{"ashby": &fakeFetcher{postings: postings}})
}
