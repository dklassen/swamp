package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

// fakeFormFetcher returns form (or err) and counts its calls.
type fakeFormFetcher struct {
	form  jobboard.ApplicationForm
	err   error
	calls int
	// deadline records whether the last call's context had a deadline.
	deadline bool
}

func (f *fakeFormFetcher) FetchApplicationForm(ctx context.Context, boardToken, jobID string) (jobboard.ApplicationForm, error) {
	f.calls++
	_, f.deadline = ctx.Deadline()
	if boardToken != "acme" || jobID != "job-1" {
		return jobboard.ApplicationForm{}, errors.New("unexpected board or job " + boardToken + "/" + jobID)
	}
	return f.form, f.err
}

var fetchedForm = jobboard.ApplicationForm{
	Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required, document.CoverLetter: jobboard.Optional},
	Questions: []jobboard.Question{{Label: "LinkedIn Profile", Required: true, Type: "text"}},
}

// formFixture is a DraftingService with fetcher registered for Greenhouse.
type formFixture struct {
	app      testutil.TestApp
	drafting *service.DraftingService
}

func newFormFixture(t *testing.T, fetcher *fakeFormFetcher) *formFixture {
	t.Helper()
	f := &formFixture{app: testutil.NewTestApp(t)}
	f.drafting = service.NewDraftingService(f.app.Store, f.app.Docs, map[string]service.FormFetcher{jobboard.Greenhouse: fetcher}, service.FormConfig{FetchTimeout: time.Second})
	return f
}

// posting adds a company on source and its posting job-1.
func (f *formFixture) posting(t *testing.T, source string) posting.Posting {
	t.Helper()
	c, err := f.app.Companies.Add(t.Context(), "Acme", source, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return testutil.MustIngestPostingFor(t, f.app, c.ID, "job-1", "Engineer")
}

func TestDraftingService_Prepare_NoStoredForm_FetchesAndStoresIt(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: fetchedForm}
	f := newFormFixture(t, fetcher)
	p := f.posting(t, jobboard.Greenhouse)

	got, err := f.drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if diff := cmp.Diff(service.FormRead{Form: &fetchedForm}, got.Form); diff != "" {
		t.Errorf("Form (-want +got):\n%s", diff)
	}
	if !fetcher.deadline {
		t.Error("the fetch had no deadline, want it bounded by the fetch timeout")
	}
	stored, err := f.app.Postings.Form(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Form: %v", err)
	}
	if diff := cmp.Diff(&fetchedForm, stored); diff != "" {
		t.Errorf("stored form (-want +got):\n%s", diff)
	}
}

// Forms aren't known to change apart from their postings, so a stored
// form is never fetched again.
func TestDraftingService_Prepare_StoredForm_IsNeverFetchedAgain(t *testing.T) {
	t.Parallel()
	stored := jobboard.ApplicationForm{Questions: []jobboard.Question{{Label: "Why us?", Required: true}}}
	for _, source := range []string{jobboard.Greenhouse, jobboard.Ashby} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			fetcher := &fakeFormFetcher{form: fetchedForm}
			f := newFormFixture(t, fetcher)
			p := f.posting(t, source)
			if err := f.app.Postings.SaveForm(t.Context(), p.ID, stored); err != nil {
				t.Fatalf("SaveForm: %v", err)
			}

			got, err := f.drafting.Prepare(t.Context(), p.ID)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if diff := cmp.Diff(service.FormRead{Form: &stored}, got.Form); diff != "" {
				t.Errorf("Form (-want +got):\n%s", diff)
			}
			if fetcher.calls != 0 {
				t.Errorf("fetches = %d, want 0", fetcher.calls)
			}
		})
	}
}

func TestDraftingService_Prepare_FormFetchFails_PreparesWithoutAForm(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{err: errors.New("board unavailable")}
	f := newFormFixture(t, fetcher)
	p := f.posting(t, jobboard.Greenhouse)

	got, err := f.drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if diff := cmp.Diff(service.FormRead{FetchError: "board unavailable"}, got.Form); diff != "" {
		t.Errorf("Form (-want +got):\n%s", diff)
	}
}

func TestDraftingService_Prepare_SourceWithoutAFetcher_NothingStored_HasNoForm(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: fetchedForm}
	f := newFormFixture(t, fetcher)
	p := f.posting(t, jobboard.Ashby)

	got, err := f.drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if diff := cmp.Diff(service.FormRead{}, got.Form); diff != "" {
		t.Errorf("Form (-want +got):\n%s", diff)
	}
	if fetcher.calls != 0 {
		t.Errorf("fetches = %d, want 0", fetcher.calls)
	}
}
