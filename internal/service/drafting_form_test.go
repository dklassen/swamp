package service_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

// formPosting adds a Greenhouse company and its posting job-1.
func formPosting(t *testing.T, app testutil.TestApp) posting.Posting {
	t.Helper()
	c, err := app.Companies.Add(t.Context(), "Acme", jobboard.Greenhouse, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return testutil.MustIngestPostingFor(t, app, c.ID, "job-1", "Engineer")
}

func TestDraftingService_Prepare_StoredForm_PreparesWithIt(t *testing.T) {
	t.Parallel()
	app := testutil.NewTestApp(t)
	p := formPosting(t, app)
	stored := jobboard.ApplicationForm{Questions: []jobboard.Question{{Label: "Why us?", Required: true}}}
	if err := app.Forms.Save(t.Context(), p.ID, stored); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := app.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if diff := cmp.Diff(&stored, got.Form); diff != "" {
		t.Errorf("Form (-want +got):\n%s", diff)
	}
}

func TestDraftingService_Prepare_NoStoredForm_PreparesWithoutOne(t *testing.T) {
	t.Parallel()
	app := testutil.NewTestApp(t)
	p := formPosting(t, app)

	got, err := app.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got.Form != nil {
		t.Errorf("Form = %+v, want nil", got.Form)
	}
}
