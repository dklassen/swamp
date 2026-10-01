package stage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// fakeFormFetcher returns form (or err) and counts its calls.
type fakeFormFetcher struct {
	form  jobboard.ApplicationForm
	err   error
	calls int
	// deadline records whether each call's context had a deadline.
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

var testForm = jobboard.ApplicationForm{
	Documents: map[documents.Type]jobboard.Requirement{documents.Resume: jobboard.Required, documents.CoverLetter: jobboard.Optional},
	Questions: []jobboard.Question{{Label: "LinkedIn Profile", Required: true, Type: "text"}},
}

// formTestStage is a Stage with fetcher registered for Greenhouse, and a
// Greenhouse posting to prepare.
func formTestStage(t *testing.T, fetcher *fakeFormFetcher) (*Stage, *store.Store, store.Posting) {
	t.Helper()
	_, s, d := newTestStage(t)
	st := New(s, d, WithFormFetchers(map[string]FormFetcher{"greenhouse": fetcher}, time.Second))
	company, err := s.CreateCompany(context.Background(), "Acme", "greenhouse", "acme")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return st, s, mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
}

func TestPrepare_FetchesAndStoresTheApplicationForm(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: testForm}
	st, s, posting := formTestStage(t, fetcher)

	got, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got.ApplicationForm == nil {
		t.Fatal("ApplicationForm = nil, want the fetched form")
	}
	if diff := cmp.Diff(testForm, *got.ApplicationForm); diff != "" {
		t.Errorf("ApplicationForm mismatch (-want +got):\n%s", diff)
	}
	if !fetcher.deadline {
		t.Error("the fetch had no deadline, want it bounded by the fetch timeout")
	}
	if _, _, ok, err := s.GetApplicationForm(context.Background(), posting.ID); err != nil || !ok {
		t.Errorf("stored form: ok %v, err %v; want it stored", ok, err)
	}
}

// A form fetched in the last week is reused; an older one is fetched again.
func TestPrepare_RefetchesOnlyAStaleForm(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: testForm}
	st, _, posting := formTestStage(t, fetcher)
	ctx := context.Background()

	for range 2 {
		if _, err := st.Prepare(ctx, posting.ID); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetches after two prepares = %d, want 1 (the second reuses the stored form)", fetcher.calls)
	}

	st.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if _, err := st.Prepare(ctx, posting.ID); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if fetcher.calls != 2 {
		t.Errorf("fetches after the form went stale = %d, want 2", fetcher.calls)
	}
}

// A failed fetch doesn't stop stage_prepare: it returns without a form,
// saying why, and the skill drafts as it always has.
func TestPrepare_FetchFailureReturnsWithoutAForm(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{err: errors.New("board unavailable")}
	st, _, posting := formTestStage(t, fetcher)

	got, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got.ApplicationForm != nil {
		t.Errorf("ApplicationForm = %+v, want nil", got.ApplicationForm)
	}
	if got.ApplicationFormError == "" {
		t.Error("ApplicationFormError is empty, want the fetch failure")
	}
}

// A board with no form fetcher (Ashby, Lever: #167) gets no form and no
// error: there's nothing to fetch.
func TestPrepare_NoFormForBoardsWithoutAFetcher(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFormFetcher{form: testForm}
	_, s, d := newTestStage(t)
	st := New(s, d, WithFormFetchers(map[string]FormFetcher{"greenhouse": fetcher}, time.Second))
	company := mustCreateCompany(t, s, "Ashby Co")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")

	got, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got.ApplicationForm != nil || got.ApplicationFormError != "" || fetcher.calls != 0 {
		t.Errorf("ApplicationForm %+v, error %q, fetches %d; want none of them", got.ApplicationForm, got.ApplicationFormError, fetcher.calls)
	}
}

// The form's JSON is part of the agent contract (SKILL.md reads it), so a
// Go-side rename has to fail a test rather than silently change it.
func TestApplicationForm_JSONShape(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(testForm)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"Documents":{"cover_letter":"optional","resume":"required"},"Questions":[{"Label":"LinkedIn Profile","Required":true,"Type":"text"}]}`
	if string(got) != want {
		t.Errorf("json.Marshal(form) = %s\nwant %s", got, want)
	}
}
