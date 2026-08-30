// Package sync orchestrates refreshing a company's postings: fetch from
// its job board, gate new postings through the company's filters, upsert
// into store, and reconcile open/closed status against what the fetch
// actually returned.
package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store"
)

// Posting is sync's own source-agnostic posting shape -- what every
// PostingFetcher must translate its source's native posting type into.
// Kept as its own type (rather than reusing e.g. ashby.Posting) so sync
// doesn't depend on any one job board's client; each source package
// (ashby, greenhouse) keeps its own native Posting type matching what
// that source's API actually returns, and a small per-source adapter
// (AshbyFetcher, GreenhouseFetcher) translates into this shape. Not
// every source can populate every field -- e.g. Greenhouse has no
// employment/workplace type -- those are simply left empty.
type Posting struct {
	SourceID        string
	Title           string
	Department      string
	Team            string
	Location        string
	EmploymentType  string
	WorkplaceType   string
	DescriptionHTML string
	DescriptionText string
	JobURL          string
	ApplicationURL  string
	PublishedAt     time.Time
	RawPayload      []byte
}

// PostingFetcher is sync's own minimal view of a job board client, one
// per source. boardSlug is whatever that source's client needs to
// identify the board (an Ashby slug, a Greenhouse board token, etc.) --
// it's passed through from store.Company.SourceRef untouched.
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
