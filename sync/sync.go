// Package sync orchestrates refreshing a company's postings: fetch from
// its job board, gate new postings through the company's filters, upsert
// into store, and reconcile open/closed status against what the fetch
// actually returned.
package sync

import (
	"context"
	"fmt"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// Posting is an alias to jobboard.Posting, not a separate struct.
// ashby.Posting/greenhouse.Posting/lever.Posting are the same alias, so a
// source client's Posting IS this type -- no field-by-field translation
// code exists between a client and sync, and none can silently drop a
// field. Not every source can populate every field -- e.g. Greenhouse has
// no employment/workplace type -- those are simply left empty (see
// jobboard's doc comment, and decisions.log, #57).
type Posting = jobboard.Posting

// PostingFetcher is sync's own minimal view of a job board client, one
// per source. Every source client (*ashby.Client, *greenhouse.Client,
// *lever.Client) already has this exact method signature, so each one
// satisfies PostingFetcher directly -- no per-source adapter type is
// needed. boardSlug is whatever that source's client needs to identify
// the board (an Ashby slug, a Greenhouse board token, etc.) -- it's
// passed through from store.Company.SourceRef untouched.
type PostingFetcher interface {
	FetchPostings(ctx context.Context, boardSlug string) ([]Posting, error)
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

// Syncer routes each company to the PostingFetcher for its source
// (company.Source, e.g. "ashby" or "greenhouse") -- see SyncCompany.
type Syncer struct {
	store    *store.Store
	fetchers map[string]PostingFetcher
}

func New(s *store.Store, fetchers map[string]PostingFetcher) *Syncer {
	return &Syncer{store: s, fetchers: fetchers}
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
