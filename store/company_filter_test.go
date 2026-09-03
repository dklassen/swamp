package store

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateCompanyFilter_ThenList_ReturnsCreatedFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	created, err := s.CreateCompanyFilter(ctx, acme.ID, "department", "Engineering")
	if err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}

	got, err := s.ListCompanyFilters(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}

	want := []CompanyFilter{created}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListCompanyFilters mismatch (-want +got):\n%s", diff)
	}
}

func TestListCompanyFilters_OnlyReturnsFiltersForThatCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")

	acmeFilter, err := s.CreateCompanyFilter(ctx, acme.ID, "department", "Engineering")
	if err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	if _, err := s.CreateCompanyFilter(ctx, globex.ID, "location", "Remote"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}

	got, err := s.ListCompanyFilters(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}

	want := []CompanyFilter{acmeFilter}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListCompanyFilters mismatch (-want +got):\n%s", diff)
	}
}

func TestReplaceCompanyFilters_ReplacesExistingFiltersWithNewSet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(ctx, acme.ID, "department", "Sales"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}

	if err := s.ReplaceCompanyFilters(ctx, acme.ID, []CompanyFilter{
		{Field: "department", Value: "Engineering"},
		{Field: "location", Value: "Remote"},
	}); err != nil {
		t.Fatalf("ReplaceCompanyFilters: %v", err)
	}

	got, err := s.ListCompanyFilters(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListCompanyFilters after replace = %+v, want 2 (old Sales filter gone)", got)
	}
	for _, f := range got {
		if f.Field == "department" && f.Value != "Engineering" {
			t.Errorf("department filter value = %q, want %q", f.Value, "Engineering")
		}
		if f.Field == "location" && f.Value != "Remote" {
			t.Errorf("location filter value = %q, want %q", f.Value, "Remote")
		}
	}
}

func TestReplaceCompanyFilters_EmptySet_LeavesNoFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(ctx, acme.ID, "department", "Sales"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}

	if err := s.ReplaceCompanyFilters(ctx, acme.ID, nil); err != nil {
		t.Fatalf("ReplaceCompanyFilters: %v", err)
	}

	got, err := s.ListCompanyFilters(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListCompanyFilters after replacing with empty set = %+v, want empty", got)
	}
}

func TestDeleteCompanyFilters_RemovesAllFiltersForCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(ctx, acme.ID, "department", "Engineering"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	if _, err := s.CreateCompanyFilter(ctx, acme.ID, "location", "Remote"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}

	if err := s.DeleteCompanyFilters(ctx, acme.ID); err != nil {
		t.Fatalf("DeleteCompanyFilters: %v", err)
	}

	got, err := s.ListCompanyFilters(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListCompanyFilters after delete = %+v, want empty", got)
	}
}
