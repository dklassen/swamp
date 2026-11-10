package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/company"
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

// formFixture is a PostingService with fetcher registered for Greenhouse,
// whose clock the test moves with now.
type formFixture struct {
	app      testutil.TestApp
	postings *service.PostingService
	now      time.Time
}

func newFormFixture(t *testing.T, fetcher *fakeFormFetcher) *formFixture {
	t.Helper()
	f := &formFixture{app: testutil.NewTestApp(t), now: time.Now()}
	f.postings = service.NewPostingService(f.app.Store, map[string]service.FormFetcher{jobboard.Greenhouse: fetcher}, service.FormConfig{
		FetchTimeout: time.Second,
		Now:          func() time.Time { return f.now },
	})
	return f
}

// posting adds a company on source and its posting job-1.
func (f *formFixture) posting(t *testing.T, source string) (posting.Posting, company.Company) {
	t.Helper()
	c, err := f.app.Companies.Add(t.Context(), "Acme", source, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return testutil.MustIngestPostingFor(t, f.app, c.ID, "job-1", "Engineer"), *c
}

func TestPostingService_CurrentForm_NoStoredForm_FetchesAndStoresIt(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: fetchedForm}
	f := newFormFixture(t, fetcher)
	p, c := f.posting(t, jobboard.Greenhouse)

	got, err := f.postings.CurrentForm(t.Context(), p, c)
	if err != nil {
		t.Fatalf("CurrentForm: %v", err)
	}
	if diff := cmp.Diff(&service.FormRead{Form: &fetchedForm}, got); diff != "" {
		t.Errorf("CurrentForm (-want +got):\n%s", diff)
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

func TestPostingService_CurrentForm_FreshStoredForm_IsReused(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: fetchedForm}
	f := newFormFixture(t, fetcher)
	p, c := f.posting(t, jobboard.Greenhouse)
	if _, err := f.postings.CurrentForm(t.Context(), p, c); err != nil {
		t.Fatalf("CurrentForm: %v", err)
	}
	f.now = f.now.Add(6 * 24 * time.Hour)

	got, err := f.postings.CurrentForm(t.Context(), p, c)
	if err != nil {
		t.Fatalf("CurrentForm: %v", err)
	}
	if diff := cmp.Diff(&service.FormRead{Form: &fetchedForm}, got); diff != "" {
		t.Errorf("CurrentForm (-want +got):\n%s", diff)
	}
	if fetcher.calls != 1 {
		t.Errorf("fetches = %d, want 1: the second read reuses the stored form", fetcher.calls)
	}
}

func TestPostingService_CurrentForm_StaleStoredForm_IsFetchedAndStoredAgain(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: fetchedForm}
	f := newFormFixture(t, fetcher)
	p, c := f.posting(t, jobboard.Greenhouse)
	if _, err := f.postings.CurrentForm(t.Context(), p, c); err != nil {
		t.Fatalf("CurrentForm: %v", err)
	}
	f.now = f.now.Add(8 * 24 * time.Hour)
	changed := jobboard.ApplicationForm{Questions: []jobboard.Question{{Label: "Why us?", Required: true, Type: "long_text"}}}
	fetcher.form = changed

	got, err := f.postings.CurrentForm(t.Context(), p, c)
	if err != nil {
		t.Fatalf("CurrentForm: %v", err)
	}
	if diff := cmp.Diff(&service.FormRead{Form: &changed}, got); diff != "" {
		t.Errorf("CurrentForm (-want +got):\n%s", diff)
	}
	stored, err := f.app.Postings.Form(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Form: %v", err)
	}
	if diff := cmp.Diff(&changed, stored); diff != "" {
		t.Errorf("stored form (-want +got):\n%s", diff)
	}
}

func TestPostingService_CurrentForm_FetchFails_ReadsWithoutAForm(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{err: errors.New("board unavailable")}
	f := newFormFixture(t, fetcher)
	p, c := f.posting(t, jobboard.Greenhouse)

	got, err := f.postings.CurrentForm(t.Context(), p, c)
	if err != nil {
		t.Fatalf("CurrentForm: %v", err)
	}
	if diff := cmp.Diff(&service.FormRead{FetchError: "board unavailable"}, got); diff != "" {
		t.Errorf("CurrentForm (-want +got):\n%s", diff)
	}
}

func TestPostingService_CurrentForm_FetchFails_ReadsTheStaleStoredForm(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: fetchedForm}
	f := newFormFixture(t, fetcher)
	p, c := f.posting(t, jobboard.Greenhouse)
	if _, err := f.postings.CurrentForm(t.Context(), p, c); err != nil {
		t.Fatalf("CurrentForm: %v", err)
	}
	f.now = f.now.Add(8 * 24 * time.Hour)
	fetcher.err = errors.New("board unavailable")

	got, err := f.postings.CurrentForm(t.Context(), p, c)
	if err != nil {
		t.Fatalf("CurrentForm: %v", err)
	}
	if diff := cmp.Diff(&service.FormRead{Form: &fetchedForm, FetchError: "board unavailable"}, got); diff != "" {
		t.Errorf("CurrentForm (-want +got):\n%s", diff)
	}
}

// A source with no fetcher only has forms entered by hand, which nothing
// can refresh, so they're read however old.
func TestPostingService_CurrentForm_SourceWithoutAFetcher(t *testing.T) {
	t.Parallel()
	entered := jobboard.ApplicationForm{Questions: []jobboard.Question{{Label: "Why us?", Required: true}}}
	tests := []struct {
		name    string
		entered *jobboard.ApplicationForm
		want    *service.FormRead
	}{
		{name: "nothing entered: no form", want: &service.FormRead{}},
		{name: "entered a year ago: that form", entered: &entered, want: &service.FormRead{Form: &entered}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fetcher := &fakeFormFetcher{form: fetchedForm}
			f := newFormFixture(t, fetcher)
			p, c := f.posting(t, jobboard.Ashby)
			if tt.entered != nil {
				if err := f.app.Postings.SaveForm(t.Context(), p.ID, *tt.entered); err != nil {
					t.Fatalf("SaveForm: %v", err)
				}
			}
			f.now = f.now.Add(365 * 24 * time.Hour)

			got, err := f.postings.CurrentForm(t.Context(), p, c)
			if err != nil {
				t.Fatalf("CurrentForm: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("CurrentForm (-want +got):\n%s", diff)
			}
			if fetcher.calls != 0 {
				t.Errorf("fetches = %d, want 0", fetcher.calls)
			}
		})
	}
}
