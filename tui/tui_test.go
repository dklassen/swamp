package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// assertEventFrom checks what every event a command sends has: an ID, a
// time in UTC, and src as its source.
func assertEventFrom(t *testing.T, meta message.Meta, src message.Source) {
	t.Helper()
	if meta.ID == "" {
		t.Error("Meta.ID is empty, want one")
	}
	if meta.At.IsZero() || meta.At.Location() != time.UTC {
		t.Errorf("Meta.At = %v, want a time in UTC", meta.At)
	}
	if meta.Source != src {
		t.Errorf("Meta.Source = %+v, want %+v", meta.Source, src)
	}
}

// synced is a company's sync reporting back from src: a step of run runID,
// or a refresh for 0, with code saying why it didn't sync, zero if it did.
func synced(src message.Source, companyID int64, runID int, code message.ErrorCode) tea.Msg {
	return message.New(src, message.Subject{Type: message.TypeCompany, ID: companyID}, message.OpNone, time.Now().UTC(),
		message.Ran[message.SyncReport]{Value: message.SyncReport{RunID: runID, Code: code}})
}

func mustCreateCompany(t *testing.T, s testutil.TestApp, name, source, sourceRef string) company.Company {
	t.Helper()
	c, err := s.Companies.Add(context.Background(), name, source, sourceRef)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return *c
}

// mustUpsertPosting creates a posting directly via the posting service,
// bypassing the syncer -- for tests that need a posting/application to
// exist without driving a full sync.
func mustUpsertPosting(t *testing.T, s testutil.TestApp, companyID int64, sourceID, title string) posting.Posting {
	t.Helper()
	ingested, err := s.Postings.Ingest(context.Background(), companyID, jobboard.Ashby, sourceID, posting.IngestedFields{Title: title})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return ingested.Posting
}

// storedPosting is posting id as the store has it, with the fields a
// snapshot leaves out.
func storedPosting(t *testing.T, s testutil.TestApp, id int64) posting.Posting {
	t.Helper()
	p, err := s.Postings.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return *p
}

func mustCreateApplication(t *testing.T, s testutil.TestApp, postingID int64) application.Application {
	t.Helper()
	a, err := createApplicationIn(context.Background(), s, postingID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	return *a
}

// fakeFetcher is a sync.PostingFetcher whose FetchPostings return value is
// configured per test, so tui tests never make real HTTP calls.
type fakeFetcher struct {
	postings  map[string][]jobboard.Posting
	errBoards map[string]error // boardSlug -> error FetchPostings returns for it
}

func (f *fakeFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	if err, ok := f.errBoards[boardSlug]; ok {
		return nil, err
	}
	return f.postings[boardSlug], nil
}

// newAppOn is an App on s's services, syncer and docs.
func newAppOn(s testutil.TestApp, syncer *sync.Syncer, docs *documents.Store) *App {
	return New(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, s.Changes, syncer, docs, Screens)
}

// storeOf is app's services, for tests that get only an App from their
// setup. Its Store and Path are empty.
func storeOf(app *App) testutil.TestApp {
	return testutil.TestApp{
		Companies:    app.deps.companies,
		Postings:     app.deps.postings,
		Applications: app.deps.applications,
		Documents:    app.deps.documentSvc,
		Scores:       app.deps.scores,
		Changes:      app.changeLogService,
	}
}

func newTestSyncer(s testutil.TestApp, postings map[string][]jobboard.Posting) *sync.Syncer {
	return sync.New(s.Companies, s.Postings, map[string]sync.PostingFetcher{jobboard.Ashby: &fakeFetcher{postings: postings}}, sync.DefaultConfig())
}

// mustDoc is status.Doc(documentType), failing the test on an error.
func mustDoc(t *testing.T, status documents.Status, documentType document.Type) documents.Doc {
	t.Helper()
	doc, err := status.Doc(documentType)
	if err != nil {
		t.Fatalf("Doc(%s): %v", documentType, err)
	}
	return doc
}

func addCompanyFilter(t *testing.T, s testutil.TestApp, companyID int64, field, value string) {
	t.Helper()
	ctx := context.Background()
	existing, err := s.Companies.Filters(ctx, companyID)
	if err != nil {
		t.Fatalf("add company filter %s=%s: %v", field, value, err)
	}
	if err := s.Companies.SetFilters(ctx, companyID, append(existing, filter.Filter{Field: field, Value: value})); err != nil {
		t.Fatalf("add company filter %s=%s: %v", field, value, err)
	}
}

func recordScores(ctx context.Context, s testutil.TestApp, provenance score.Provenance, entries []score.Entry) (*score.RecordResult, error) {
	return s.Scores.Record(ctx, provenance, entries)
}

func createReview(ctx context.Context, s testutil.TestApp, applicationID int64, documentType document.Type, content string, outcome document.Outcome, notes string) (*document.Review, error) {
	return s.Documents.Review(ctx, applicationID, documentType, content, outcome, notes)
}

func createApplicationIn(ctx context.Context, s testutil.TestApp, postingID int64) (*application.Application, error) {
	return s.Postings.ApplyTo(ctx, postingID)
}

// updateApplicationStatusIn sets the status of postingID's application.
func updateApplicationStatusIn(ctx context.Context, s testutil.TestApp, postingID int64, status application.Status) error {
	app, err := s.Postings.Application(ctx, postingID)
	if err != nil {
		return err
	}
	_, err = s.Applications.SetStatus(ctx, app.ID, status)
	return err
}

// readsOf is the Read[T]s cmd sends.
func readsOf[T message.Body](t *testing.T, cmd tea.Cmd) []message.Event[message.Read[T]] {
	t.Helper()
	var reads []message.Event[message.Read[T]]
	for _, msg := range runCmd(t, cmd) {
		if read, ok := msg.(message.Event[message.Read[T]]); ok {
			reads = append(reads, read)
		}
	}
	return reads
}

// assertReadsOnceAtATime checks that changed, heard twice in a row, reads
// T once, and once more when that read arrives, and no more after.
func assertReadsOnceAtATime[T message.Body](t *testing.T, r *model.MsgRouter, changed tea.Msg) {
	t.Helper()
	first := r.Dispatch(changed)
	if more := readsOf[T](t, r.Dispatch(changed)); len(more) != 0 {
		t.Errorf("a second write while the read was in flight sent %d reads, want 0", len(more))
	}
	read := readsOf[T](t, first)
	if len(read) != 1 {
		t.Fatalf("a write sent %d reads, want 1", len(read))
	}
	again := readsOf[T](t, r.Dispatch(read[0]))
	if len(again) != 1 {
		t.Fatalf("the read arriving sent %d more, want 1 for the write heard during it", len(again))
	}
	if more := readsOf[T](t, r.Dispatch(again[0])); len(more) != 0 {
		t.Errorf("the second read arriving sent %d more, want 0", len(more))
	}
}
