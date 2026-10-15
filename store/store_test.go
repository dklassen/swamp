package store

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/store/sqlcdb"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return newTestStoreAt(t, t.TempDir()+"/test.db")
}

// newTestStoreAt is newTestStore on the database at path, so a test can
// open a second Store on the same file (another process, in effect).
func newTestStoreAt(t *testing.T, path string) *Store {
	t.Helper()

	// Open, not sql.Open, so tests run with the same connection settings
	// as swamp itself.
	sqlDB, err := Open(path, DefaultConfig())
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

	return New(sqlDB)
}

func mustCreateCompany(t *testing.T, s *Store, name, source, sourceRef string) Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, source, sourceRef)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func mustUpsertPosting(t *testing.T, s *Store, companyID int64, sourceID, title string) Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), CreatePostingParams{
		CompanyID: companyID,
		Source:    "ashby",
		SourceID:  sourceID,
		IngestedFields: IngestedFields{
			Title:      title,
			RawPayload: `{"id":"` + sourceID + `"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

// createCompanyFilter sets up a filter for store's own tests, which can't
// import companyfilter: it imports store.
func (s *Store) createCompanyFilter(ctx context.Context, companyID int64, field, value string) (sqlcdb.CompanyFilter, error) {
	return s.queries.CreateCompanyFilter(ctx, sqlcdb.CreateCompanyFilterParams{CompanyID: companyID, Field: field, Value: value})
}
