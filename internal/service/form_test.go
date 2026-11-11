package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

var fetchedForm = jobboard.ApplicationForm{
	Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required, document.CoverLetter: jobboard.Optional},
	Questions: []jobboard.Question{{Label: "LinkedIn Profile", Required: true, Type: "text"}},
}

// boardForms is a board's forms by job ID, recording each fetch as
// "board/job". during, if set, runs inside each fetch.
type boardForms struct {
	mu      sync.Mutex
	forms   map[string]jobboard.ApplicationForm
	fetched []string
	during  func()
}

func (b *boardForms) FetchApplicationForm(_ context.Context, boardToken, jobID string) (jobboard.ApplicationForm, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.during != nil {
		b.during()
	}
	b.fetched = append(b.fetched, boardToken+"/"+jobID)
	form, ok := b.forms[jobID]
	if !ok {
		return jobboard.ApplicationForm{}, errors.New("no job " + jobID)
	}
	return form, nil
}

// newFormJob is a TestApp and a FormService fetching Ashby forms from board.
func newFormJob(t *testing.T, board *boardForms) (testutil.TestApp, *service.FormService) {
	t.Helper()
	app := testutil.NewTestApp(t)
	return app, service.NewFormService(app.Store, map[string]service.FormFetcher{jobboard.Ashby: board}, service.FormConfig{FetchTimeout: time.Second})
}

func TestFormService_FetchMissing_StoresANewPostingsForm(t *testing.T) {
	t.Parallel()
	board := &boardForms{forms: map[string]jobboard.ApplicationForm{"job-1": fetchedForm}}
	app, forms := newFormJob(t, board)
	acme := testutil.MustAddCompany(t, app, "acme")
	p := testutil.MustIngestPostingFor(t, app, acme.ID, "job-1", "Engineer")

	got, err := forms.FetchMissing(t.Context(), acme)
	if err != nil {
		t.Fatalf("FetchMissing: %v", err)
	}
	if diff := cmp.Diff(&service.FormFetch{Fetched: 1}, got); diff != "" {
		t.Errorf("FetchMissing (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"acme/job-1"}, board.fetched); diff != "" {
		t.Errorf("fetched (-want +got):\n%s", diff)
	}
	stored, err := forms.Get(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(&fetchedForm, stored); diff != "" {
		t.Errorf("stored form (-want +got):\n%s", diff)
	}
}

func TestFormService_FetchMissing_CountsAFailedFetchAndRetriesIt(t *testing.T) {
	t.Parallel()
	board := &boardForms{forms: map[string]jobboard.ApplicationForm{"job-1": fetchedForm}}
	app, forms := newFormJob(t, board)
	acme := testutil.MustAddCompany(t, app, "acme")
	testutil.MustIngestPostingFor(t, app, acme.ID, "job-1", "Engineer")
	p2 := testutil.MustIngestPostingFor(t, app, acme.ID, "job-2", "Designer")

	got, err := forms.FetchMissing(t.Context(), acme)
	if err != nil {
		t.Fatalf("first FetchMissing: %v", err)
	}
	if diff := cmp.Diff(&service.FormFetch{Fetched: 1, Failed: 1}, got); diff != "" {
		t.Errorf("first FetchMissing (-want +got):\n%s", diff)
	}

	board.forms["job-2"] = fetchedForm
	board.fetched = nil
	got, err = forms.FetchMissing(t.Context(), acme)
	if err != nil {
		t.Fatalf("second FetchMissing: %v", err)
	}
	if diff := cmp.Diff(&service.FormFetch{Fetched: 1}, got); diff != "" {
		t.Errorf("second FetchMissing (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"acme/job-2"}, board.fetched); diff != "" {
		t.Errorf("second fetched (-want +got):\n%s", diff)
	}
	if _, err := forms.Get(t.Context(), p2.ID); err != nil {
		t.Errorf("Get job-2's form: %v", err)
	}
}

func TestFormService_FetchMissing_SkipsAClosedPosting(t *testing.T) {
	t.Parallel()
	board := &boardForms{forms: map[string]jobboard.ApplicationForm{"job-1": fetchedForm}}
	app, forms := newFormJob(t, board)
	acme := testutil.MustAddCompany(t, app, "acme")
	p := testutil.MustIngestPostingFor(t, app, acme.ID, "job-1", "Engineer")
	if err := app.Postings.Close(t.Context(), p.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := forms.FetchMissing(t.Context(), acme)
	if err != nil {
		t.Fatalf("FetchMissing: %v", err)
	}
	if diff := cmp.Diff(&service.FormFetch{}, got); diff != "" {
		t.Errorf("FetchMissing (-want +got):\n%s", diff)
	}
	if len(board.fetched) != 0 {
		t.Errorf("fetched %v, want no fetch", board.fetched)
	}
}

// Postings are Ashby's in these tests, so a FormService with fetchers for
// other sources only has none for theirs.
func TestFormService_FetchMissing_SourceWithoutAFetcher_MakesNoRequest(t *testing.T) {
	t.Parallel()
	app := testutil.NewTestApp(t)
	board := &boardForms{forms: map[string]jobboard.ApplicationForm{"job-1": fetchedForm}}
	forms := service.NewFormService(app.Store, map[string]service.FormFetcher{jobboard.Greenhouse: board}, service.FormConfig{FetchTimeout: time.Second})
	acme := testutil.MustAddCompany(t, app, "acme")
	p := testutil.MustIngestPostingFor(t, app, acme.ID, "job-1", "Engineer")

	got, err := forms.FetchMissing(t.Context(), acme)
	if err != nil {
		t.Fatalf("FetchMissing: %v", err)
	}
	if diff := cmp.Diff(&service.FormFetch{}, got); diff != "" {
		t.Errorf("FetchMissing (-want +got):\n%s", diff)
	}
	if len(board.fetched) != 0 {
		t.Errorf("fetched %v, want no fetch", board.fetched)
	}
	if _, err := forms.Get(t.Context(), p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get: err = %v, want store.ErrNotFound", err)
	}
}

// slowBoard holds each fetch long enough for every fetch allowed to start
// to be in flight together, recording the most it saw.
type slowBoard struct {
	mu       sync.Mutex
	inFlight int
	most     int
}

func (b *slowBoard) FetchApplicationForm(context.Context, string, string) (jobboard.ApplicationForm, error) {
	b.mu.Lock()
	b.inFlight++
	b.most = max(b.most, b.inFlight)
	b.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	b.mu.Lock()
	b.inFlight--
	b.mu.Unlock()
	return fetchedForm, nil
}

func TestFormService_FetchMissing_FetchesFourAtATime(t *testing.T) {
	t.Parallel()
	app := testutil.NewTestApp(t)
	board := &slowBoard{}
	forms := service.NewFormService(app.Store, map[string]service.FormFetcher{jobboard.Ashby: board}, service.FormConfig{FetchTimeout: time.Second})
	acme := testutil.MustAddCompany(t, app, "acme")
	for _, id := range []string{"job-1", "job-2", "job-3", "job-4", "job-5", "job-6", "job-7", "job-8"} {
		testutil.MustIngestPostingFor(t, app, acme.ID, id, "Engineer")
	}

	got, err := forms.FetchMissing(t.Context(), acme)
	if err != nil {
		t.Fatalf("FetchMissing: %v", err)
	}
	if diff := cmp.Diff(&service.FormFetch{Fetched: 8}, got); diff != "" {
		t.Errorf("FetchMissing (-want +got):\n%s", diff)
	}
	if board.most != 4 {
		t.Errorf("at most %d fetches at once, want 4", board.most)
	}
}

func TestFormService_FetchMissing_KeepsAFormSavedDuringTheFetch(t *testing.T) {
	t.Parallel()
	byHand := jobboard.ApplicationForm{Questions: []jobboard.Question{{Label: "Portfolio", Type: "text"}}}
	board := &boardForms{forms: map[string]jobboard.ApplicationForm{"job-1": fetchedForm}}
	app, forms := newFormJob(t, board)
	acme := testutil.MustAddCompany(t, app, "acme")
	p := testutil.MustIngestPostingFor(t, app, acme.ID, "job-1", "Engineer")
	board.during = func() {
		if err := forms.Save(t.Context(), p.ID, byHand); err != nil {
			t.Errorf("Save: %v", err)
		}
	}

	if _, err := forms.FetchMissing(t.Context(), acme); err != nil {
		t.Fatalf("FetchMissing: %v", err)
	}
	stored, err := forms.Get(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(&byHand, stored); diff != "" {
		t.Errorf("stored form (-want +got):\n%s", diff)
	}
}

func TestFormService_FetchMissing_LeavesAStoredFormAlone(t *testing.T) {
	t.Parallel()
	byHand := jobboard.ApplicationForm{Questions: []jobboard.Question{{Label: "Portfolio", Type: "text"}}}
	tests := []struct {
		name  string
		store func(*testing.T, *service.FormService, company.Company, int64)
		want  jobboard.ApplicationForm
	}{
		{"saved by hand", func(t *testing.T, forms *service.FormService, _ company.Company, postingID int64) {
			t.Helper()
			if err := forms.Save(t.Context(), postingID, byHand); err != nil {
				t.Fatalf("Save: %v", err)
			}
		}, byHand},
		{"fetched earlier", func(t *testing.T, forms *service.FormService, c company.Company, _ int64) {
			t.Helper()
			if _, err := forms.FetchMissing(t.Context(), c); err != nil {
				t.Fatalf("first FetchMissing: %v", err)
			}
		}, fetchedForm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			board := &boardForms{forms: map[string]jobboard.ApplicationForm{"job-1": fetchedForm}}
			app, forms := newFormJob(t, board)
			acme := testutil.MustAddCompany(t, app, "acme")
			p := testutil.MustIngestPostingFor(t, app, acme.ID, "job-1", "Engineer")
			tt.store(t, forms, acme, p.ID)
			board.fetched = nil

			got, err := forms.FetchMissing(t.Context(), acme)
			if err != nil {
				t.Fatalf("FetchMissing: %v", err)
			}
			if diff := cmp.Diff(&service.FormFetch{}, got); diff != "" {
				t.Errorf("FetchMissing (-want +got):\n%s", diff)
			}
			if len(board.fetched) != 0 {
				t.Errorf("fetched %v, want no fetch", board.fetched)
			}
			stored, err := forms.Get(t.Context(), p.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if diff := cmp.Diff(&tt.want, stored); diff != "" {
				t.Errorf("stored form (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFormService_Get(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")

	none, err := s.Forms.Get(ctx, p.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get before any save: err = %v, want store.ErrNotFound", err)
	}
	if none != nil {
		t.Errorf("Get before any save = %+v, want nil", none)
	}

	want := jobboard.ApplicationForm{
		Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required},
		Questions: []jobboard.Question{{Label: "Why us?", Required: true, Type: "long_text"}},
	}
	if err := s.Forms.Save(ctx, p.ID, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Forms.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(&want, got); diff != "" {
		t.Errorf("Get (-want +got):\n%s", diff)
	}
}

// A form saved by hand replaces whatever was stored, fetched or not.
func TestFormService_Save_ReplacesTheStoredForm(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")

	first := jobboard.ApplicationForm{Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required}}
	if err := s.Forms.Save(ctx, p.ID, first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	want := jobboard.ApplicationForm{
		Documents: map[document.Type]jobboard.Requirement{document.CoverLetter: jobboard.Optional},
		Questions: []jobboard.Question{{Label: "Why us?", Required: true, Type: "long_text"}},
	}
	if err := s.Forms.Save(ctx, p.ID, want); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	got, err := s.Forms.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(&want, got); diff != "" {
		t.Errorf("Get (-want +got):\n%s", diff)
	}
}
