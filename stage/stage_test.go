package stage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

func newTestStage(t *testing.T) (*Stage, *store.Store, *documents.Store) {
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

	s := store.New(sqlDB)
	d := documents.NewStore(t.TempDir())
	return New(s, d), s, d
}

func mustCreateCompany(t *testing.T, s *store.Store, name string) store.Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, "ashby", name)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func mustUpsertPosting(t *testing.T, s *store.Store, companyID int64, sourceID, title string) store.Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{
		CompanyID:  companyID,
		Source:     "ashby",
		SourceID:   sourceID,
		Title:      title,
		RawPayload: `{"id":"` + sourceID + `"}`,
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

func mustMarkInterested(t *testing.T, s *store.Store, postingID int64) {
	t.Helper()
	if _, err := s.SetPostingInterested(context.Background(), postingID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
}

func TestList_ExcludesPostingNotMarkedInterested(t *testing.T) {
	t.Parallel()

	st, s, _ := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")

	got, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d candidates, want 0 (posting was never marked interested)", len(got))
	}
}

func TestList_ExcludesArchivedPostingEvenIfOnceInterested(t *testing.T) {
	t.Parallel()

	st, s, _ := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)
	if _, err := s.SetPostingArchived(context.Background(), posting.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}

	got, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d candidates, want 0 (posting is archived)", len(got))
	}
}

func TestList_IncludesInterestedPostingWithNoApplication(t *testing.T) {
	t.Parallel()

	st, s, _ := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)

	got, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	c := got[0]
	if c.Posting.ID != posting.ID {
		t.Errorf("Posting.ID = %d, want %d", c.Posting.ID, posting.ID)
	}
	if c.CompanyName != "Acme" {
		t.Errorf("CompanyName = %q, want %q", c.CompanyName, "Acme")
	}
	if c.ApplicationID != nil {
		t.Errorf("ApplicationID = %v, want nil (no application started)", c.ApplicationID)
	}
}

func TestList_IncludesInterestedPostingWithApplicationButNoDocuments(t *testing.T) {
	t.Parallel()

	st, s, _ := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)
	app, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	got, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	if got[0].ApplicationID == nil || *got[0].ApplicationID != app.ID {
		t.Errorf("ApplicationID = %v, want %d", got[0].ApplicationID, app.ID)
	}
}

func TestList_ExcludesPostingWithBothDocumentsAlreadyGenerated(t *testing.T) {
	t.Parallel()

	st, s, d := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)
	app, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	paths, err := d.EnsureDir(app.ID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(paths.CoverLetter, []byte("letter"), 0o644); err != nil {
		t.Fatalf("write cover letter: %v", err)
	}
	if err := os.WriteFile(paths.Resume, []byte("resume"), 0o644); err != nil {
		t.Fatalf("write resume: %v", err)
	}

	got, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d candidates, want 0 (both documents already exist)", len(got))
	}
}

func TestList_IncludesPostingWithOnlyOneDocumentGenerated(t *testing.T) {
	t.Parallel()

	st, s, d := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)
	app, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	paths, err := d.EnsureDir(app.ID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(paths.CoverLetter, []byte("letter"), 0o644); err != nil {
		t.Fatalf("write cover letter: %v", err)
	}

	got, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1 (resume is still missing)", len(got))
	}
}

func TestPrepare_CreatesApplicationWhenNoneExists(t *testing.T) {
	t.Parallel()

	st, s, _ := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)

	prepared, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if prepared.ApplicationID == 0 {
		t.Error("ApplicationID = 0, want a created application id")
	}

	app, err := s.GetApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if app.ID != prepared.ApplicationID {
		t.Errorf("stored application id = %d, want %d", app.ID, prepared.ApplicationID)
	}
}

func TestPrepare_IsIdempotentForExistingApplication(t *testing.T) {
	t.Parallel()

	st, s, _ := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)

	first, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	second, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("second Prepare: %v", err)
	}

	if second.ApplicationID != first.ApplicationID {
		t.Errorf("second.ApplicationID = %d, want %d (should reuse the same application)", second.ApplicationID, first.ApplicationID)
	}
}

func TestPrepare_CreatesDocumentDirectory(t *testing.T) {
	t.Parallel()

	st, s, _ := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)

	prepared, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	info, err := os.Stat(filepath.Dir(prepared.CoverLetter.Path))
	if err != nil {
		t.Fatalf("Stat document dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected the document directory to exist")
	}
	if prepared.CoverLetter.Exists {
		t.Error("CoverLetter.Exists = true, want false (nothing written yet)")
	}
	if prepared.Resume.Exists {
		t.Error("Resume.Exists = true, want false (nothing written yet)")
	}
}

func TestPrepare_ReflectsDocumentsWrittenBetweenCalls(t *testing.T) {
	t.Parallel()

	st, s, _ := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)

	first, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	if err := os.WriteFile(first.CoverLetter.Path, []byte("letter"), 0o644); err != nil {
		t.Fatalf("write cover letter: %v", err)
	}

	second, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	if !second.CoverLetter.Exists {
		t.Error("CoverLetter.Exists = false, want true (written between calls)")
	}
	if second.Resume.Exists {
		t.Error("Resume.Exists = true, want false (never written)")
	}
}
