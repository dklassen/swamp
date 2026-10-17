package sync

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/seed"
	"github.com/dklassen/swamp/store"
)

// toIngestedFields builds a posting.IngestedFields from a fetched Posting -- the single
// place that conversion happens (#57 and #67).
func toIngestedFields(p jobboard.Posting) posting.IngestedFields {
	return posting.IngestedFields{
		Title:           p.Title,
		Department:      p.Department,
		Team:            p.Team,
		Location:        p.Location,
		EmploymentType:  p.EmploymentType,
		WorkplaceType:   p.WorkplaceType,
		DescriptionHTML: p.DescriptionHTML,
		DescriptionText: p.DescriptionText,
		JobURL:          p.JobURL,
		ApplicationURL:  p.ApplicationURL,
		PublishedAt:     posting.OptionalTime{Time: p.PublishedAt},
		RawPayload:      string(p.RawPayload),
	}
}

// SyncCompany refreshes a single company's postings: fetches its board
// and upserts every posting it lists into store.
//
// Cancelling ctx stops the fetch, never the writes (#149). Before the
// board answers, nothing has been written, so abandoning it is safe;
// after, the sync is all local writes bounded by the database's busy
// timeout, and stopping them partway would leave the company half-synced.
//
// Only one sync of a company runs at a time, across every process sharing
// the database (#150): SyncCompany holds the company's sync lease from
// before the fetch until after the last write, and returns
// ErrSyncInProgress, having fetched and written nothing, if another sync
// holds it. Two runs that fetched the board at different moments could
// otherwise each act on their own view -- the older one closing a posting
// the newer one had just reopened, and ending its application with it.
//
// A sync is atomic per posting, not as a whole (RFC 0003; a whole-sync
// transaction was rejected there). An error after the fetch returns at
// once, and leaves:
//   - every posting change already made committed, each in its own
//     transaction (#147, #148), and postings not yet reached untouched.
//     The one change made in two transactions is a closed posting whose
//     content changed (Ingest, then Reopen): stopped
//     between them it's left updated but still closed, a valid state;
//   - the close pass not run, so no posting is closed;
//   - the company not marked fetched, so "last fetched" stays stale;
//   - no Result: callers report the error.
//
// Every write is conditional and safe to repeat, so the next clean sync
// finishes the job.
func (s *Syncer) SyncCompany(ctx context.Context, companyID int64) (result *Result, err error) {
	company, err := s.companies.Get(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("sync: get company: %w", err)
	}
	fetcher, ok := s.fetchers[company.Source]
	if !ok {
		return nil, fmt.Errorf("sync: unsupported source %q", company.Source)
	}

	token := rand.Text()
	taken, err := s.companies.AcquireSyncLease(ctx, companyID, token, s.cfg.LeaseTimeout)
	if err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	if !taken {
		return nil, ErrSyncInProgress
	}
	s.trackLease(token, companyID)
	defer func() {
		s.untrackLease(token)
		// Released whether the sync succeeded or not, and even if ctx was
		// cancelled. If this fails the lease stays held until it expires,
		// which is worth reporting.
		if releaseErr := s.companies.ReleaseSyncLease(context.WithoutCancel(ctx), companyID, token); releaseErr != nil {
			result, err = nil, errors.Join(err, fmt.Errorf("sync: %w", releaseErr))
		}
	}()

	held, err := s.syncHeld(ctx, *company, fetcher, Result{CompanyID: companyID, Name: company.Name})
	if err != nil {
		return nil, err
	}
	return &held, nil
}

// syncHeld is SyncCompany's work once it holds the company's lease: fetch,
// save every posting (ingest), close what's gone (closeMissing,
// reconcile.go).
func (s *Syncer) syncHeld(ctx context.Context, company company.Company, fetcher PostingFetcher, result Result) (Result, error) {
	companyID := company.ID
	raw, err := s.fetch(ctx, fetcher, company.SourceRef)
	if err != nil {
		return result, fmt.Errorf("sync: fetch postings: %w", err)
	}
	ctx = context.WithoutCancel(ctx)
	result.Fetched = len(raw)
	// A copy, not in place: the fetcher may hand the same slice to other syncs.
	fetched := make([]jobboard.Posting, len(raw))
	for i, p := range raw {
		fetched[i] = sanitizePosting(p)
	}

	result, err = s.ingest(ctx, company, fetched, result)
	if err != nil {
		// The close pass doesn't run after a failed ingest: see
		// SyncCompany's partial-failure contract.
		return result, err
	}
	result, err = s.closeMissing(ctx, company, fetched, result)
	if err != nil {
		return result, err
	}

	if err := s.companies.MarkFetched(ctx, companyID); err != nil {
		return result, fmt.Errorf("sync: mark company fetched: %w", err)
	}
	return result, nil
}

// ingest saves every fetched posting, whatever the company's filters say
// (they only decide what's shown): creating it, updating its content, or
// reopening it if it was closed. It stops at the first error.
func (s *Syncer) ingest(ctx context.Context, company company.Company, fetched []jobboard.Posting, result Result) (Result, error) {
	for _, p := range fetched {
		ingested, err := s.postings.Ingest(ctx, company.ID, company.Source, p.SourceID, toIngestedFields(p))
		if err != nil {
			return result, fmt.Errorf("sync: save posting: %w", err)
		}
		if ingested.Created {
			result.Created++
		}
		if ingested.Updated {
			result.Updated++
		}
		if ingested.Posting.ListingStatus == "closed" {
			if err := s.postings.Reopen(ctx, ingested.Posting.ID); err != nil {
				return result, fmt.Errorf("sync: reopen posting: %w", err)
			}
			result.Reopened++
		}
	}
	return result, nil
}

// CreateCompany creates a company after confirming sourceRef resolves to
// a real board on source's API (issue #36), so a typo'd slug is rejected
// up front instead of surfacing later as a fetch error, or silently as
// zero postings. It fails closed: any fetch error, including a timeout,
// blocks the create. The fetched postings are discarded; ingestion waits
// for the next sync.
// Shared by the TUI's add-company form and ImportCompanies.
func (s *Syncer) CreateCompany(ctx context.Context, name, source, sourceRef string) (*company.Company, error) {
	fetcher, ok := s.fetchers[source]
	if !ok {
		return nil, fmt.Errorf("sync: unsupported source %q", source)
	}
	if _, err := s.fetch(ctx, fetcher, sourceRef); err != nil {
		return nil, fmt.Errorf("sync: %s/%s does not resolve to a real board: %w", source, sourceRef, err)
	}
	created, err := s.companies.Add(ctx, name, source, sourceRef)
	if err != nil {
		return nil, fmt.Errorf("sync: create company: %w", err)
	}
	return created, nil
}

// ImportResult reports the outcome of importing one seed.Entry.
// Company is the zero value when Err is set.
type ImportResult struct {
	Name      string
	Source    string
	SourceRef string
	Company   company.Company
	Err       error
}

// ImportCompanies bulk-creates companies from a parsed seed file. Each
// entry goes through CreateCompany, so it's validated against its
// source's real API before being saved (issue #36): an unsupported
// Source or a SourceRef that the board rejects fails that entry without
// creating a row, and without aborting the rest of the batch -- same
// per-item isolation as SyncAll. A successfully-fetched entry's postings
// are discarded, not ingested -- exactly like adding a company through
// the TUI, ingestion happens on the next fetch, not at creation time.
// CompanyService.Add is already idempotent on (source, source_ref), so
// re-running the same seed file is always safe.
// An entry's optional Description is applied only when the company has none,
// so re-importing never overwrites a description written since.
func (s *Syncer) ImportCompanies(ctx context.Context, entries []seed.Entry) []ImportResult {
	results := make([]ImportResult, len(entries))
	for i, e := range entries {
		results[i] = ImportResult{Name: e.Name, Source: e.Source, SourceRef: e.SourceRef}

		company, err := s.CreateCompany(ctx, e.Name, e.Source, e.SourceRef)
		if err != nil {
			results[i].Err = err
			continue
		}
		// Fill in the description only if the company has none, so re-importing
		// never overwrites one written since (same rule as AddCompany).
		if company.Description == "" && e.Description != "" {
			updated, err := s.companies.SetDescription(ctx, company.ID, e.Description)
			if err != nil {
				results[i].Err = fmt.Errorf("sync: set company description: %w", err)
				continue
			}
			company = updated
		}
		results[i].Company = *company
	}
	return results
}

// AddCompanyOutcome says what AddCompany did.
type AddCompanyOutcome int

const (
	// AddCompanyCreated: the company is new and was added.
	AddCompanyCreated AddCompanyOutcome = iota
	// AddCompanySkippedDeleted: the user deleted this company before, so it
	// was left deleted.
	AddCompanySkippedDeleted
	// AddCompanyAlreadyExists: the company was already being tracked. Its
	// description is filled in if it had none, never overwritten.
	AddCompanyAlreadyExists
)

// AddCompanyResult reports what AddCompany did. OpenJobs is how many
// postings the board listed when it was checked.
type AddCompanyResult struct {
	Outcome  AddCompanyOutcome
	Company  company.Company
	OpenJobs int
}

// AddCompany adds one company discovered by an agent (the MCP add_company
// tool). Unlike ImportCompanies and the TUI, which both go through
// CreateCompany, it never restores a company the user deleted and never
// renames an existing one: an agent re-discovering a company mustn't undo
// the user's decisions about it. A new company's slug is checked against
// its board first, the same live check ImportCompanies uses (#36), and the
// fetched postings are only counted, not ingested -- like any other way of
// adding a company, ingestion waits for the next sync, after the user has
// had a chance to set filters.
func (s *Syncer) AddCompany(ctx context.Context, name, source, sourceRef, description string) (*AddCompanyResult, error) {
	existing, err := s.companies.GetBySource(ctx, source, sourceRef)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// New company: fall through to the board check and create.
	case err != nil:
		return nil, fmt.Errorf("sync: look up company: %w", err)
	case existing.DeletedAt != nil:
		return &AddCompanyResult{Outcome: AddCompanySkippedDeleted, Company: *existing}, nil
	default:
		if existing.Description == "" && description != "" {
			existing, err = s.companies.SetDescription(ctx, existing.ID, description)
			if err != nil {
				return nil, fmt.Errorf("sync: set company description: %w", err)
			}
		}
		return &AddCompanyResult{Outcome: AddCompanyAlreadyExists, Company: *existing}, nil
	}

	fetcher, ok := s.fetchers[source]
	if !ok {
		return nil, fmt.Errorf("sync: unsupported source %q", source)
	}
	postings, err := s.fetch(ctx, fetcher, sourceRef)
	if err != nil {
		return nil, fmt.Errorf("sync: %s/%s does not resolve to a real board: %w", source, sourceRef, err)
	}

	company, err := s.companies.Add(ctx, name, source, sourceRef)
	if err != nil {
		return nil, fmt.Errorf("sync: create company: %w", err)
	}
	company, err = s.companies.SetDescription(ctx, company.ID, description)
	if err != nil {
		return nil, fmt.Errorf("sync: set company description: %w", err)
	}
	return &AddCompanyResult{Outcome: AddCompanyCreated, Company: *company, OpenJobs: len(postings)}, nil
}
