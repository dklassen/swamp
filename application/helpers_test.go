package application_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/store"
)

// testStore keeps the *sql.DB beside the store, for tests that read or
// write past the area's functions.
type testStore struct {
	*store.Store
	sqlDB *sql.DB
}

func newTestStore(t *testing.T) testStore {
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
	return testStore{Store: store.New(sqlDB), sqlDB: sqlDB}
}

func (s testStore) countRows(t *testing.T, table string, applicationID int64) int {
	t.Helper()
	var n int
	if err := s.sqlDB.QueryRow(`SELECT count(*) FROM `+table+` WHERE application_id = ?`, applicationID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func mustCreateCompany(t *testing.T, s testStore, name, source, sourceRef string) store.Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, source, sourceRef)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func mustUpsertPosting(t *testing.T, s testStore, companyID int64, sourceID, title string) store.Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{
		CompanyID: companyID, Source: "ashby", SourceID: sourceID,
		IngestedFields: store.IngestedFields{Title: title, RawPayload: "{}"},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

func createApplication(ctx context.Context, s testStore, postingID int64) (application.Application, error) {
	var app application.Application
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		app, err = application.Create(ctx, tx, postingID)
		return err
	})
	return app, err
}

func mustCreateApplication(t *testing.T, s testStore, postingID int64) application.Application {
	t.Helper()
	a, err := createApplication(context.Background(), s, postingID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return a
}

func updateApplicationStatus(ctx context.Context, s testStore, postingID int64, status applicationstatus.Status) (application.Application, error) {
	var app application.Application
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		app, err = application.UpdateStatus(ctx, tx, postingID, status)
		return err
	})
	return app, err
}

// closePosting closes a posting and ends its application the way sync
// does: both in one transaction, the application only if the posting
// really closed.
func closePosting(ctx context.Context, s testStore, postingID int64, from []applicationstatus.Status) (closed, ended bool, err error) {
	err = store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		if closed, err = store.ClosePosting(ctx, tx, postingID); err != nil || !closed {
			return err
		}
		ended, err = application.EndForPosting(ctx, tx, postingID, from)
		return err
	})
	return closed, ended, err
}
