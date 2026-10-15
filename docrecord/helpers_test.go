package docrecord_test

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/docrecord"
	"github.com/dklassen/swamp/documents"
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

func mustCreateCompany(t *testing.T, s *store.Store, name, source, sourceRef string) store.Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, source, sourceRef)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func mustUpsertPosting(t *testing.T, s *store.Store, companyID int64, sourceID, title string) store.Posting {
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

func mustCreateApplication(t *testing.T, s *store.Store, postingID int64) application.Application {
	t.Helper()
	a, err := createApplication(context.Background(), s, postingID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	return a
}

// createReview runs CreateReview in its own transaction, as a caller does.
func createReview(ctx context.Context, s *store.Store, applicationID int64, documentType documents.Type, content string, outcome docrecord.Outcome, notes string) (docrecord.Review, error) {
	var review docrecord.Review
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		review, err = docrecord.CreateReview(ctx, tx, applicationID, documentType, content, outcome, notes)
		return err
	})
	return review, err
}

func createApplication(ctx context.Context, s *store.Store, postingID int64) (application.Application, error) {
	var app application.Application
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		app, err = application.Create(ctx, tx, postingID)
		return err
	})
	return app, err
}
