package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// SetFilters replaces the company's filters, in order, and touches no
// other company's.
func TestCompanyService_SetFilters_ReplacesTheCompanysFilters(t *testing.T) {
	t.Parallel()
	sales := filter.Filter{Field: filter.FieldDepartment, Value: "Sales"}
	engineering := filter.Filter{Field: filter.FieldDepartment, Value: "Engineering"}
	remote := filter.Filter{Field: filter.FieldLocation, Value: "Remote"}
	tests := []struct {
		name   string
		before []filter.Filter
		set    []filter.Filter
	}{
		{"none before: saved in order", nil, []filter.Filter{engineering, remote}},
		{"some before: replaced", []filter.Filter{sales}, []filter.Filter{remote}},
		{"set to none: removed", []filter.Filter{sales}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			companies := s.Companies
			acme := testutil.MustAddCompany(t, s, "acme")
			globex := testutil.MustAddCompany(t, s, "globex")
			others := []filter.Filter{sales}
			if err := companies.SetFilters(ctx, globex.ID, others); err != nil {
				t.Fatalf("SetFilters globex: %v", err)
			}
			if err := companies.SetFilters(ctx, acme.ID, tt.before); err != nil {
				t.Fatalf("SetFilters before: %v", err)
			}

			if err := companies.SetFilters(ctx, acme.ID, tt.set); err != nil {
				t.Fatalf("SetFilters: %v", err)
			}

			got, err := companies.Filters(ctx, acme.ID)
			if err != nil {
				t.Fatalf("Filters: %v", err)
			}
			if diff := cmp.Diff(tt.set, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Filters (-want +got):\n%s", diff)
			}
			got, err = companies.Filters(ctx, globex.ID)
			if err != nil {
				t.Fatalf("Filters globex: %v", err)
			}
			if diff := cmp.Diff(others, got); diff != "" {
				t.Errorf("the other company's filters (-want +got):\n%s", diff)
			}
		})
	}
}

// The CHECK on company_filters.field makes the second insert fail after
// the delete has run.
func TestCompanyService_SetFilters_FailingInsert_KeepsTheOldFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	companies := s.Companies
	acme := testutil.MustAddCompany(t, s, "acme")
	old := []filter.Filter{{Field: filter.FieldDepartment, Value: "Sales"}}
	if err := companies.SetFilters(ctx, acme.ID, old); err != nil {
		t.Fatalf("SetFilters: %v", err)
	}

	bad := []filter.Filter{{Field: filter.FieldLocation, Value: "Remote"}, {Field: "unsupported", Value: "x"}}
	if err := companies.SetFilters(ctx, acme.ID, bad); err == nil {
		t.Fatal("SetFilters with an unsupported field: err = nil, want an error")
	}

	got, err := companies.Filters(ctx, acme.ID)
	if err != nil {
		t.Fatalf("Filters: %v", err)
	}
	if diff := cmp.Diff(old, got); diff != "" {
		t.Errorf("Filters after a failed SetFilters (-want +got):\n%s", diff)
	}
}

// A deleted company is never synced, so Delete closes its open postings
// itself, with a "closed" history row each; its applications are left
// alone.
func TestCompanyService_Delete_ClosesItsOpenPostings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	postings := s.Postings
	acme := testutil.MustAddCompany(t, s, "Acme")
	globex := testutil.MustAddCompany(t, s, "Globex")
	open := testutil.MustIngestPostingFor(t, s, acme.ID, "job-1", "Software Engineer")
	alreadyClosed := testutil.MustIngestPostingFor(t, s, acme.ID, "job-2", "Designer")
	if err := postings.Close(ctx, alreadyClosed.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	otherCompany := testutil.MustIngestPostingFor(t, s, globex.ID, "job-3", "Engineer")
	app := testutil.MustApplyTo(t, s, open.ID)
	type state struct {
		Posting posting.Posting
		History []string
	}
	stateOf := func(postingID int64) state {
		t.Helper()
		p, err := postings.Get(ctx, postingID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		history, err := postings.History(ctx, postingID)
		if err != nil {
			t.Fatalf("History: %v", err)
		}
		st := state{Posting: *p}
		for _, h := range history {
			st.History = append(st.History, h.ChangeType)
		}
		return st
	}
	closedBefore, otherBefore := stateOf(alreadyClosed.ID), stateOf(otherCompany.ID)

	if _, err := s.Companies.Delete(ctx, acme.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got := stateOf(open.ID)
	if got.Posting.ListingStatus != closedBefore.Posting.ListingStatus {
		t.Errorf("open posting's ListingStatus = %q, want %q", got.Posting.ListingStatus, closedBefore.Posting.ListingStatus)
	}
	if diff := cmp.Diff([]string{posting.ChangeClosed}, got.History); diff != "" {
		t.Errorf("open posting's history (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(closedBefore, stateOf(alreadyClosed.ID)); diff != "" {
		t.Errorf("already-closed posting changed (-before +after):\n%s", diff)
	}
	if diff := cmp.Diff(otherBefore, stateOf(otherCompany.ID)); diff != "" {
		t.Errorf("another company's posting changed (-before +after):\n%s", diff)
	}
	appAfter, err := postings.Application(ctx, open.ID)
	if err != nil {
		t.Fatalf("Application: %v", err)
	}
	if diff := cmp.Diff(app, *appAfter); diff != "" {
		t.Errorf("application changed (-before +after):\n%s", diff)
	}
}

// Before is read in SetName's transaction, so it's what the last writer
// left, not what the caller last saw.
func TestCompanyService_SetName_ReturnsTheCompanyBeforeAndAfter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme := testutil.MustAddCompany(t, s, "acme")
	described, err := s.Companies.SetDescription(ctx, acme.ID, "Makes anvils")
	if err != nil {
		t.Fatalf("SetDescription: %v", err)
	}

	got, err := s.Companies.SetName(ctx, acme.ID, "Acme Corp")
	if err != nil {
		t.Fatalf("SetName: %v", err)
	}

	if diff := cmp.Diff(*described, got.Before); diff != "" {
		t.Errorf("Before (-want +got):\n%s", diff)
	}
	after, err := s.Companies.Get(ctx, acme.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(*after, got.After); diff != "" {
		t.Errorf("After (-want +got):\n%s", diff)
	}
	if !got.At.Equal(got.After.UpdatedAt) || got.At.Location() != time.UTC {
		t.Errorf("At = %v, want After.UpdatedAt %v, in UTC", got.At, got.After.UpdatedAt)
	}
}

func TestCompanyService_Writes_ReturnNotFoundForACompanyNotThere(t *testing.T) {
	t.Parallel()
	writes := []struct {
		name  string
		write func(*service.CompanyService, context.Context, int64) (*change.Of[company.Company], error)
	}{
		{"SetName", func(s *service.CompanyService, ctx context.Context, id int64) (*change.Of[company.Company], error) {
			return s.SetName(ctx, id, "Acme Corp")
		}},
		{"Delete", (*service.CompanyService).Delete},
	}
	companies := []struct {
		name    string
		arrange func(*testing.T, testutil.TestApp) int64
	}{
		{"missing", func(*testing.T, testutil.TestApp) int64 { return 999 }},
		{"already deleted", func(t *testing.T, s testutil.TestApp) int64 {
			t.Helper()
			acme := testutil.MustAddCompany(t, s, "acme")
			if _, err := s.Companies.Delete(context.Background(), acme.ID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			return acme.ID
		}},
	}
	for _, w := range writes {
		for _, c := range companies {
			t.Run(w.name+", "+c.name, func(t *testing.T) {
				t.Parallel()
				s := testutil.NewTestApp(t)
				id := c.arrange(t, s)

				got, err := w.write(s.Companies, context.Background(), id)

				if !errors.Is(err, store.ErrNotFound) {
					t.Errorf("err = %v, want store.ErrNotFound", err)
				}
				if got != nil {
					t.Errorf("change = %+v, want nil", got)
				}
			})
		}
	}
}

// After is the soft-deleted row, and At when it was deleted.
func TestCompanyService_Delete_ReturnsTheCompanyBeforeAndAfter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme := testutil.MustAddCompany(t, s, "acme")
	described, err := s.Companies.SetDescription(ctx, acme.ID, "Makes anvils")
	if err != nil {
		t.Fatalf("SetDescription: %v", err)
	}

	got, err := s.Companies.Delete(ctx, acme.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if diff := cmp.Diff(*described, got.Before); diff != "" {
		t.Errorf("Before (-want +got):\n%s", diff)
	}
	after, err := s.Companies.GetIncludingDeleted(ctx, acme.ID)
	if err != nil {
		t.Fatalf("GetIncludingDeleted: %v", err)
	}
	if diff := cmp.Diff(*after, got.After); diff != "" {
		t.Errorf("After (-want +got):\n%s", diff)
	}
	if got.After.DeletedAt == nil {
		t.Fatal("After.DeletedAt = nil, want when it was deleted")
	}
	if !got.At.Equal(*got.After.DeletedAt) || got.At.Location() != time.UTC {
		t.Errorf("At = %v, want After.DeletedAt %v, in UTC", got.At, *got.After.DeletedAt)
	}
}

func TestCompanyService_GetManyIncludingDeleted_FindsDeletedLeavesOutMissing(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	live, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	gone, err := s.Companies.Add(ctx, "Globex", jobboard.Ashby, "globex")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	deleted, err := s.Companies.Delete(ctx, gone.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := s.Companies.GetManyIncludingDeleted(ctx, []int64{gone.ID, 9999, live.ID})
	if err != nil {
		t.Fatalf("GetManyIncludingDeleted: %v", err)
	}
	want := []company.Company{*live, deleted.After}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b company.Company) bool { return a.ID < b.ID })); diff != "" {
		t.Errorf("GetManyIncludingDeleted (-want +got):\n%s", diff)
	}
}

// ListedPostings lists a company's open postings its filters let through,
// newest first, leaving out archived ones unless they're included.
func TestCompanyService_ListedPostings_ListsWhatTheCompanysListShows(t *testing.T) {
	t.Parallel()
	engineering := []filter.Filter{{Field: filter.FieldDepartment, Value: "Engineering"}}
	for _, tt := range []struct {
		name            string
		includeArchived bool
		// change hides other, a Sales posting, or its company; listed is
		// what's listed after it, in order.
		change      func(t *testing.T, s testutil.TestApp, companyID, other int64)
		listed      func(kept, other int64) []int64
		wantFilters []filter.Filter
	}{
		{"nothing hidden", false, func(*testing.T, testutil.TestApp, int64, int64) {},
			func(kept, other int64) []int64 { return []int64{other, kept} }, nil},
		{"closed", false, func(t *testing.T, s testutil.TestApp, _, other int64) {
			if err := s.Postings.Close(context.Background(), other, nil); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}, func(kept, _ int64) []int64 { return []int64{kept} }, nil},
		{"filtered out", false, func(t *testing.T, s testutil.TestApp, companyID, _ int64) {
			if err := s.Companies.SetFilters(context.Background(), companyID, engineering); err != nil {
				t.Fatalf("SetFilters: %v", err)
			}
		}, func(kept, _ int64) []int64 { return []int64{kept} }, engineering},
		{"archived, hidden", false, func(t *testing.T, s testutil.TestApp, _, other int64) {
			if _, err := s.Postings.SetArchived(context.Background(), other); err != nil {
				t.Fatalf("SetArchived: %v", err)
			}
		}, func(kept, _ int64) []int64 { return []int64{kept} }, nil},
		{"archived, included", true, func(t *testing.T, s testutil.TestApp, _, other int64) {
			if _, err := s.Postings.SetArchived(context.Background(), other); err != nil {
				t.Fatalf("SetArchived: %v", err)
			}
		}, func(kept, other int64) []int64 { return []int64{other, kept} }, nil},
		{"company deleted", false, func(t *testing.T, s testutil.TestApp, companyID, _ int64) {
			if _, err := s.Companies.Delete(context.Background(), companyID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
		}, func(int64, int64) []int64 { return nil }, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			acme := testutil.MustAddCompany(t, s, "Acme")
			globex := testutil.MustAddCompany(t, s, "Globex")
			ingest := func(companyID int64, sourceID, department string) int64 {
				t.Helper()
				ingested, err := s.Postings.Ingest(ctx, companyID, jobboard.Ashby, sourceID, posting.IngestedFields{Title: "Engineer", Department: department, RawPayload: "{}"})
				if err != nil {
					t.Fatalf("Ingest: %v", err)
				}
				return ingested.Posting.ID
			}
			kept := ingest(acme.ID, "job-1", "Engineering")
			other := ingest(acme.ID, "job-2", "Sales")
			ingest(globex.ID, "job-3", "Engineering")
			tt.change(t, s, acme.ID, other)

			got, err := s.Companies.ListedPostings(ctx, acme.ID, service.PostingListPage{IncludeArchived: tt.includeArchived, Limit: 10})
			if err != nil {
				t.Fatalf("ListedPostings: %v", err)
			}
			var ids []int64
			for _, l := range got.Page.Listings {
				ids = append(ids, l.ID)
			}
			if diff := cmp.Diff(tt.listed(kept, other), ids); diff != "" {
				t.Errorf("listed postings (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantFilters, got.Filters, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Filters (-want +got):\n%s", diff)
			}
		})
	}
}

// A page holds the newest Limit postings and says how many there are in
// all.
func TestCompanyService_ListedPostings_MoreThanAPage_SaysHowManyInAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme := testutil.MustAddCompany(t, s, "Acme")
	var ids []int64
	for _, sourceID := range []string{"job-1", "job-2", "job-3"} {
		ids = append(ids, testutil.MustIngestPostingFor(t, s, acme.ID, sourceID, "Engineer").ID)
	}

	got, err := s.Companies.ListedPostings(ctx, acme.ID, service.PostingListPage{Limit: 2})
	if err != nil {
		t.Fatalf("ListedPostings: %v", err)
	}
	var listed []int64
	for _, l := range got.Page.Listings {
		listed = append(listed, l.ID)
	}
	if diff := cmp.Diff([]int64{ids[2], ids[1]}, listed); diff != "" {
		t.Errorf("listed postings (-want +got):\n%s", diff)
	}
	if !got.Page.HasMore || got.Page.Total != len(ids) {
		t.Errorf("HasMore, Total = %v, %d; want true, %d", got.Page.HasMore, got.Page.Total, len(ids))
	}
}

// With archived postings hidden, a company's list has as many postings in
// all as its Open count says.
func TestCompanyService_ListedPostings_TotalIsTheOpenCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	postings := s.Postings
	acme := testutil.MustAddCompany(t, s, "Acme")
	ingest := func(sourceID, department string) int64 {
		t.Helper()
		ingested, err := postings.Ingest(ctx, acme.ID, jobboard.Ashby, sourceID, posting.IngestedFields{Title: "Engineer", Department: department, RawPayload: "{}"})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		return ingested.Posting.ID
	}
	ingest("job-1", "Engineering")
	ingest("job-2", "Engineering")
	ingest("job-3", "Sales")
	if err := postings.Close(ctx, ingest("job-4", "Engineering"), nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := postings.SetArchived(ctx, ingest("job-5", "Engineering")); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	if err := s.Companies.SetFilters(ctx, acme.ID, []filter.Filter{{Field: filter.FieldDepartment, Value: "Engineering"}}); err != nil {
		t.Fatalf("SetFilters: %v", err)
	}

	got, err := s.Companies.ListedPostings(ctx, acme.ID, service.PostingListPage{Limit: 1})
	if err != nil {
		t.Fatalf("ListedPostings: %v", err)
	}
	counts, err := postings.CountOpenByCompany(ctx)
	if err != nil {
		t.Fatalf("CountOpenByCompany: %v", err)
	}
	if got.Page.Total != counts[acme.ID] || got.Page.Total == 0 {
		t.Errorf("Total = %d, Open count = %d; want them equal and not 0", got.Page.Total, counts[acme.ID])
	}
}

// A company's filters on one field let through a posting that equals any
// of them, ignoring case; filters on two fields must both let it through.
func TestCompanyService_ListedPostings_AppliesTheCompanysFilters(t *testing.T) {
	t.Parallel()
	department := func(v string) filter.Filter { return filter.Filter{Field: filter.FieldDepartment, Value: v} }
	location := func(v string) filter.Filter { return filter.Filter{Field: filter.FieldLocation, Value: v} }
	type where struct{ department, location string }
	for _, tt := range []struct {
		name    string
		filters []filter.Filter
		posting where
		listed  bool
	}{
		{"no filters", nil, where{"Sales", "Remote"}, true},
		{"its department", []filter.Filter{department("Engineering")}, where{"Engineering", "Remote"}, true},
		{"another department", []filter.Filter{department("Engineering")}, where{"Sales", "Remote"}, false},
		{"its department in another case", []filter.Filter{department("Engineering")}, where{"engineering", "Remote"}, true},
		{"one of two departments", []filter.Filter{department("Engineering"), department("Data Engineering")}, where{"Data Engineering", ""}, true},
		{"neither of two departments", []filter.Filter{department("Engineering"), department("Data Engineering")}, where{"Sales", ""}, false},
		{"its department and location", []filter.Filter{department("Engineering"), location("Remote")}, where{"Engineering", "Remote"}, true},
		{"its department, another location", []filter.Filter{department("Engineering"), location("Remote")}, where{"Engineering", "New York"}, false},
		{"another department, its location", []filter.Filter{department("Engineering"), location("Remote")}, where{"Sales", "Remote"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			acme := testutil.MustAddCompany(t, s, "Acme")
			if _, err := s.Postings.Ingest(ctx, acme.ID, jobboard.Ashby, "job-1", posting.IngestedFields{Title: "Engineer", Department: tt.posting.department, Location: tt.posting.location, RawPayload: "{}"}); err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if err := s.Companies.SetFilters(ctx, acme.ID, tt.filters); err != nil {
				t.Fatalf("SetFilters: %v", err)
			}

			got, err := s.Companies.ListedPostings(ctx, acme.ID, service.PostingListPage{Limit: 10})
			if err != nil {
				t.Fatalf("ListedPostings: %v", err)
			}
			if listed := len(got.Page.Listings) == 1; listed != tt.listed {
				t.Errorf("listed = %v, want %v", listed, tt.listed)
			}
		})
	}
}
