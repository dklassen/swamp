// Package sync orchestrates refreshing a company's postings: fetch from
// its job board, gate new postings through the company's filters, upsert
// into store, and reconcile open/closed status against what the fetch
// actually returned.
package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// PostingFetcher is sync's own minimal view of a job board client, one
// per source. Every source client (*ashby.Client, *greenhouse.Client,
// *lever.Client) returns jobboard.Posting directly (not a locally-declared
// type), so each one satisfies PostingFetcher directly -- no per-source
// adapter type is needed, and no field-by-field translation code exists
// between a client and sync. Not every source can populate every field --
// e.g. Greenhouse has no employment/workplace type -- those are simply
// left empty (see jobboard's doc comment, and decisions.log, #57).
// boardSlug is whatever that source's client needs to identify the board
// (an Ashby slug, a Greenhouse board token, etc.) -- it's passed through
// from store.Company.SourceRef untouched.
type PostingFetcher interface {
	FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error)
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
	// ApplicationsClosed counts applications moved to posting_closed
	// because the posting they were for was taken down -- see
	// store.ClosePosting and earlyApplicationStatuses (issues #105, #147).
	ApplicationsClosed int
	Err                error
}

// Config holds Syncer's tunable settings.
type Config struct {
	// FetchTimeout is how long one board fetch may take before it's
	// abandoned and reported as that company's error.
	FetchTimeout time.Duration
}

// DefaultConfig is the Config swamp runs with unless told otherwise.
func DefaultConfig() Config {
	return Config{FetchTimeout: 30 * time.Second}
}

// Syncer routes each company to the PostingFetcher for its source
// (company.Source, e.g. "ashby" or "greenhouse") -- see SyncCompany.
type Syncer struct {
	store    *store.Store
	fetchers map[string]PostingFetcher
	cfg      Config
}

func New(s *store.Store, fetchers map[string]PostingFetcher, cfg Config) *Syncer {
	return &Syncer{store: s, fetchers: fetchers, cfg: cfg}
}

// fetch is the one place Syncer calls a board: it gives up after
// cfg.FetchTimeout, so a board that accepts the request but never answers
// fails that company instead of stalling everything after it. The job
// board clients use http.DefaultClient, which has no timeout of its own,
// but they build requests with ctx, so the deadline cancels the request
// -- including a body still arriving (issue #142).
func (s *Syncer) fetch(ctx context.Context, fetcher PostingFetcher, boardSlug string) ([]jobboard.Posting, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.FetchTimeout)
	defer cancel()
	return fetcher.FetchPostings(ctx, boardSlug)
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
