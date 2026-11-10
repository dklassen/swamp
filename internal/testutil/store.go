// Package testutil sets up migrated databases, services and fixtures for
// tests.
package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// MigratedDBPath is a fresh database file at the latest schema, for tests
// that open it more than once, as separate processes would.
func MigratedDBPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := store.Open(path, store.DefaultConfig())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := migrations.Up(t.Context(), sqlDB); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return path
}

// OpenStore opens the database at path as a process whose writes are
// stamped with origin, closing it when the test ends. It uses store.Open,
// not sql.Open, so tests get swamp's own connection settings.
func OpenStore(t *testing.T, path, origin string) store.Store {
	t.Helper()
	cfg := store.DefaultConfig()
	cfg.Origin = origin
	sqlDB, err := store.Open(path, cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	return store.New(sqlDB)
}

func NewTestStore(t *testing.T) store.Store {
	t.Helper()
	return OpenStore(t, MigratedDBPath(t), "")
}

// TestApp is every service on one migrated database.
type TestApp struct {
	Path         string
	Store        store.Store
	Companies    *service.CompanyService
	Postings     *service.PostingService
	Applications *service.ApplicationService
	Documents    *service.DocumentService
	Docs         *documents.Store
	Scores       *service.ScoringService
	Drafting     *service.DraftingService
	Changes      *service.ChangeLog
}

func NewTestApp(t *testing.T) TestApp {
	t.Helper()
	path := MigratedDBPath(t)
	db := OpenStore(t, path, "")
	docs := documents.NewStore(t.TempDir())
	return TestApp{
		Path:         path,
		Store:        db,
		Docs:         docs,
		Companies:    service.NewCompanyService(db),
		Postings:     service.NewPostingService(db),
		Applications: service.NewApplicationService(db, docs),
		Documents:    service.NewDocumentService(db, docs),
		Scores:       service.NewScoringService(db, docs),
		Drafting:     service.NewDraftingService(db, docs, nil, service.FormConfig{}),
		Changes:      service.NewChangeLog(db),
	}
}

func MustAddCompany(t *testing.T, s TestApp, name string) company.Company {
	t.Helper()
	c, err := s.Companies.Add(t.Context(), name, jobboard.Ashby, name)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return *c
}

// MustIngestPosting adds a company and one posting for it.
func MustIngestPosting(t *testing.T, s TestApp, sourceID, title string) posting.Posting {
	t.Helper()
	acme, err := s.Companies.Add(t.Context(), "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return MustIngestPostingFor(t, s, acme.ID, sourceID, title)
}

// MustIngestPostingFor adds a posting to an existing company.
func MustIngestPostingFor(t *testing.T, s TestApp, companyID int64, sourceID, title string) posting.Posting {
	t.Helper()
	ingested, err := s.Postings.Ingest(t.Context(), companyID, jobboard.Ashby, sourceID, posting.IngestedFields{Title: title, RawPayload: "{}"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return ingested.Posting
}

func MustApplyTo(t *testing.T, s TestApp, postingID int64) application.Application {
	t.Helper()
	app, err := s.Postings.ApplyTo(t.Context(), postingID)
	if err != nil {
		t.Fatalf("ApplyTo: %v", err)
	}
	return *app
}

// MustWriteFile writes content to path as the user would, by hand.
func MustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// MustWriteProfile writes the user's profile and returns its version.
func MustWriteProfile(t *testing.T, s TestApp, content string) string {
	t.Helper()
	MustWriteFile(t, s.Docs.ProfilePath(), content)
	return document.ContentSHA256(content)
}
