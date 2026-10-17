package service_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
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
	if _, _, err := postings.Close(ctx, alreadyClosed.ID, nil); err != nil {
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
		st := state{Posting: p}
		for _, h := range history {
			st.History = append(st.History, h.ChangeType)
		}
		return st
	}
	closedBefore, otherBefore := stateOf(alreadyClosed.ID), stateOf(otherCompany.ID)

	if err := s.Companies.Delete(ctx, acme.ID); err != nil {
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
	if diff := cmp.Diff(app, appAfter); diff != "" {
		t.Errorf("application changed (-before +after):\n%s", diff)
	}
}
