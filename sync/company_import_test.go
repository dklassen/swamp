package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/seed"
)

func TestImportCompanies_ValidEntry_CreatesCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fetcher := &perBoardFetcher{
		postings: map[string][]jobboard.Posting{
			"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
		},
	}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	entries := []seed.Entry{
		{Name: "Acme", Source: "ashby", SourceRef: "acme"},
	}

	results := syncer.ImportCompanies(ctx, entries)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("results[0].Err = %v, want nil", results[0].Err)
	}
	if results[0].Company.Name != "Acme" {
		t.Errorf("results[0].Company.Name = %q, want %q", results[0].Company.Name, "Acme")
	}

	companies, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 1 {
		t.Fatalf("got %d companies in store, want 1", len(companies))
	}
}

func TestImportCompanies_UnsupportedSource_NoCompanyCreated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	syncer := New(s, map[string]PostingFetcher{"ashby": &perBoardFetcher{}})

	entries := []seed.Entry{
		{Name: "Acme", Source: "workday", SourceRef: "acme"},
	}

	results := syncer.ImportCompanies(ctx, entries)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("results[0].Err = nil, want error for unsupported source")
	}

	companies, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 0 {
		t.Fatalf("got %d companies in store, want 0", len(companies))
	}
}

func TestImportCompanies_InvalidSourceRef_NoCompanyCreated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fetcher := &perBoardFetcher{
		errBoards: map[string]error{
			"bogus": errors.New("404 job not found"),
		},
	}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	entries := []seed.Entry{
		{Name: "Acme", Source: "ashby", SourceRef: "bogus"},
	}

	results := syncer.ImportCompanies(ctx, entries)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("results[0].Err = nil, want error for invalid source_ref")
	}

	companies, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 0 {
		t.Fatalf("got %d companies in store, want 0", len(companies))
	}
}

func TestImportCompanies_MixedBatch_GoodEntryStillImportedAfterBadOne(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fetcher := &perBoardFetcher{
		postings: map[string][]jobboard.Posting{
			"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
		},
		errBoards: map[string]error{
			"bogus": errors.New("404 job not found"),
		},
	}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	entries := []seed.Entry{
		{Name: "Bogus Co", Source: "ashby", SourceRef: "bogus"},
		{Name: "Acme", Source: "ashby", SourceRef: "acme"},
	}

	results := syncer.ImportCompanies(ctx, entries)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Err == nil {
		t.Error("results[0].Err = nil, want error for bogus entry")
	}
	if results[1].Err != nil {
		t.Errorf("results[1].Err = %v, want nil for acme entry", results[1].Err)
	}

	companies, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 1 {
		t.Fatalf("got %d companies in store, want 1", len(companies))
	}
}

func TestImportCompanies_ReimportExistingCompany_DoesNotDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fetcher := &perBoardFetcher{
		postings: map[string][]jobboard.Posting{
			"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
		},
	}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	entries := []seed.Entry{
		{Name: "Acme", Source: "ashby", SourceRef: "acme"},
	}

	_ = syncer.ImportCompanies(ctx, entries)
	results := syncer.ImportCompanies(ctx, entries)

	if results[0].Err != nil {
		t.Fatalf("results[0].Err = %v, want nil on reimport", results[0].Err)
	}

	companies, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 1 {
		t.Fatalf("got %d companies in store, want 1 (no duplicate)", len(companies))
	}
}
