package stage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/jobboard"
)

// fakeFormFetcher returns form, or err.
type fakeFormFetcher struct {
	form jobboard.ApplicationForm
	err  error
}

func (f *fakeFormFetcher) FetchApplicationForm(context.Context, string, string) (jobboard.ApplicationForm, error) {
	return f.form, f.err
}

var testForm = jobboard.ApplicationForm{
	Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required, document.CoverLetter: jobboard.Optional},
	Questions: []jobboard.Question{{Label: "LinkedIn Profile", Required: true, Type: "text"}},
}

// formTestStage is a Stage reading Greenhouse forms with fetcher, and a
// Greenhouse posting to prepare.
func formTestStage(t *testing.T, fetcher *fakeFormFetcher) (*Stage, posting.Posting) {
	t.Helper()
	_, s, d := newTestStage(t)
	postings := service.NewPostingService(s.Store, map[string]service.FormFetcher{jobboard.Greenhouse: fetcher}, service.FormConfig{FetchTimeout: time.Second, Now: time.Now})
	st := New(s.Companies, postings, s.Applications, s.Documents, s.Scores, d)
	company, err := s.Companies.Add(context.Background(), "Acme", jobboard.Greenhouse, "acme")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return st, mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
}

func TestPrepare_ReturnsTheApplicationForm(t *testing.T) {
	t.Parallel()
	st, posting := formTestStage(t, &fakeFormFetcher{form: testForm})

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
	if got.ApplicationFormError != "" {
		t.Errorf("ApplicationFormError = %q, want none", got.ApplicationFormError)
	}
}

// A failed fetch doesn't stop stage_prepare: it returns without a form,
// saying why, and the skill drafts as it always has.
func TestPrepare_FetchFailureReturnsWithoutAForm(t *testing.T) {
	t.Parallel()
	st, posting := formTestStage(t, &fakeFormFetcher{err: errors.New("board unavailable")})

	got, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got.ApplicationForm != nil {
		t.Errorf("ApplicationForm = %+v, want nil", got.ApplicationForm)
	}
	if got.ApplicationFormError != "board unavailable" {
		t.Errorf("ApplicationFormError = %q, want the fetch failure", got.ApplicationFormError)
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
