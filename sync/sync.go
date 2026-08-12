// Package sync orchestrates refreshing a company's postings: fetch from
// its job board, gate new postings through the company's filters, upsert
// into store, and reconcile open/closed status against what the fetch
// actually returned.
package sync

import (
	"context"
	"fmt"

	"github.com/dklassen/swamp/ashby"
	"github.com/dklassen/swamp/store"
)

// PostingFetcher is sync's own minimal view of a job board client:
// *ashby.Client satisfies it structurally, so tests use a fake instead of
// making real HTTP calls. Note this does not yet decouple sync from Ashby
// specifically -- the return type is ashby.Posting, since Ashby is the
// only source implemented so far. A second job board would need its own
// posting type and a real translation layer here; that's deliberately not
// built ahead of having a second real example to design it against.
type PostingFetcher interface {
	FetchPostings(ctx context.Context, boardSlug string) ([]ashby.Posting, error)
}

// Result summarizes one company's sync outcome. Err is set on a
// per-company failure (e.g. the fetch failed) without aborting a larger
// SyncAll batch.
type Result struct {
	CompanyID int64
	Fetched   int
	Created   int
	Updated   int
	Closed    int
	Reopened  int
	Err       error
}

type Syncer struct {
	store   *store.Store
	fetcher PostingFetcher
}

func New(s *store.Store, fetcher PostingFetcher) *Syncer {
	return &Syncer{store: s, fetcher: fetcher}
}

// SyncAll refreshes every active company. A single company's failure
// (e.g. its board fetch errors) is captured in that company's Result and
// does not stop the rest of the batch from being processed.
func (s *Syncer) SyncAll(ctx context.Context) ([]Result, error) {
	companies, err := s.store.ListActiveCompanies(ctx)
	if err != nil {
		return nil, fmt.Errorf("sync: list active companies: %w", err)
	}

	results := make([]Result, len(companies))
	for i, company := range companies {
		result, err := s.SyncCompany(ctx, company.ID)
		result.CompanyID = company.ID
		result.Err = err
		results[i] = result
	}
	return results, nil
}
