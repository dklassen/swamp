package sync

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/seed"
	"github.com/dklassen/swamp/store"
)

// FilterRules converts a company's saved filter rows into the rules
// filter.Match evaluates against a posting. Exported so tui's
// display-time filtering (loadPostings) shares this exact conversion
// with ingestion-time gating (SyncCompany below) instead of maintaining
// an independent copy that could silently drift out of agreement with
// this one (see decisions.log, #61).
func FilterRules(filters []store.CompanyFilter) []filter.Filter {
	rules := make([]filter.Filter, len(filters))
	for i, f := range filters {
		rules[i] = filter.Filter{Field: f.Field, Value: f.Value}
	}
	return rules
}

func toFilterPosting(p jobboard.Posting) filter.Posting {
	return filter.Posting{Department: p.Department, Location: p.Location}
}

// toIngestedFields builds the fields store.Posting and
// store.CreatePostingParams share from a fetched Posting -- the single
// place that conversion happens (see decisions.log, #57 and #67).
func toIngestedFields(p jobboard.Posting) store.IngestedFields {
	return store.IngestedFields{
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
		PublishedAt:     store.OptionalTime{Time: p.PublishedAt},
		RawPayload:      string(p.RawPayload),
	}
}

func toCreatePostingParams(companyID int64, source string, p jobboard.Posting) store.CreatePostingParams {
	return store.CreatePostingParams{
		CompanyID:      companyID,
		Source:         source,
		SourceID:       p.SourceID,
		IngestedFields: toIngestedFields(p),
	}
}

// ApplyCompanyFilters replaces companyID's saved filters and re-syncs it
// against its job board -- "changing a company's filters" as one unit,
// testable at the store+syncer boundary with no TUI dependency, rather
// than the two independently-triggered async round trips (save, then
// separately kick off a resync) tui/app.go used to glue together by
// hand. A resync is required, not optional: filter matching also gates
// ingestion (see SyncCompany below), so postings that didn't match the
// old filters were never stored at all -- narrowing what's already in
// the DB isn't enough to make a filter change fully take effect, only
// re-running ingestion under the new filters is (see decisions.log,
// #56).
func (s *Syncer) ApplyCompanyFilters(ctx context.Context, companyID int64, departments, locations []string) (Result, error) {
	filters := make([]store.CompanyFilter, 0, len(departments)+len(locations))
	for _, d := range departments {
		filters = append(filters, store.CompanyFilter{Field: filter.FieldDepartment, Value: d})
	}
	for _, l := range locations {
		filters = append(filters, store.CompanyFilter{Field: filter.FieldLocation, Value: l})
	}
	if err := s.store.ReplaceCompanyFilters(ctx, companyID, filters); err != nil {
		return Result{}, fmt.Errorf("sync: replace company filters: %w", err)
	}
	return s.SyncCompany(ctx, companyID)
}

// SyncCompany refreshes a single company's postings: fetches its board,
// gates new postings through the company's filters, and upserts matches
// into store.
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
//     content changed (IngestPosting, then ReopenPosting): stopped
//     between them it's left updated but still closed, a valid state;
//   - the close pass not run, so no posting is closed;
//   - the company not marked fetched, so "last fetched" stays stale;
//   - Result counting only what ran. Callers report the error, not the
//     counts.
//
// Every write is conditional and safe to repeat, so the next clean sync
// finishes the job.
func (s *Syncer) SyncCompany(ctx context.Context, companyID int64) (result Result, err error) {
	result = Result{CompanyID: companyID}

	company, err := s.store.GetCompany(ctx, companyID)
	if err != nil {
		return result, fmt.Errorf("sync: get company: %w", err)
	}
	result.Name = company.Name
	fetcher, ok := s.fetchers[company.Source]
	if !ok {
		return result, fmt.Errorf("sync: unsupported source %q", company.Source)
	}

	token := rand.Text()
	taken, err := s.store.AcquireSyncLease(ctx, companyID, token, s.cfg.LeaseTimeout)
	if err != nil {
		return result, fmt.Errorf("sync: %w", err)
	}
	if !taken {
		return result, ErrSyncInProgress
	}
	s.trackLease(token, companyID)
	defer func() {
		s.untrackLease(token)
		// Released whether the sync succeeded or not, and even if ctx was
		// cancelled. If this fails the lease stays held until it expires,
		// which is worth reporting.
		if releaseErr := s.store.ReleaseSyncLease(context.WithoutCancel(ctx), companyID, token); releaseErr != nil {
			err = errors.Join(err, fmt.Errorf("sync: %w", releaseErr))
		}
	}()

	return s.syncHeld(ctx, company, fetcher, result)
}

// syncHeld is SyncCompany's work once it holds the company's lease: fetch,
// save what matches the company's filters, close what's gone.
func (s *Syncer) syncHeld(ctx context.Context, company store.Company, fetcher PostingFetcher, result Result) (Result, error) {
	companyID := company.ID
	fetched, err := s.fetch(ctx, fetcher, company.SourceRef)
	if err != nil {
		return result, fmt.Errorf("sync: fetch postings: %w", err)
	}
	ctx = context.WithoutCancel(ctx)
	result.Fetched = len(fetched)
	for i := range fetched {
		fetched[i] = sanitizePosting(fetched[i])
	}

	filters, err := s.store.ListCompanyFilters(ctx, companyID)
	if err != nil {
		return result, fmt.Errorf("sync: list company filters: %w", err)
	}
	rules := FilterRules(filters)

	seenSourceIDs := make(map[string]bool, len(fetched))
	for _, p := range fetched {
		seenSourceIDs[p.SourceID] = true
	}

	for _, p := range fetched {
		matches, err := filter.Match(toFilterPosting(p), rules)
		if err != nil {
			return result, fmt.Errorf("sync: match filter: %w", err)
		}
		if !matches {
			continue
		}

		ingested, err := s.store.IngestPosting(ctx, toCreatePostingParams(company.ID, company.Source, p))
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
			reopened, err := s.store.ReopenPosting(ctx, ingested.Posting.ID)
			if err != nil {
				return result, fmt.Errorf("sync: reopen posting: %w", err)
			}
			if reopened.Reopened {
				result.Reopened++
			}
			if reopened.ApplicationRestored {
				result.ApplicationsRestored++
			}
		}
	}

	existingPostings, err := s.store.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		return result, fmt.Errorf("sync: list existing postings: %w", err)
	}
	var seen []int64
	for _, existing := range existingPostings {
		if seenSourceIDs[existing.SourceID] {
			seen = append(seen, existing.ID)
		}
	}
	// Every stored posting the board listed, including ones the filters
	// now leave out: last_seen_at is when the board last listed it (#176).
	if err := s.store.MarkPostingsSeen(ctx, seen); err != nil {
		return result, fmt.Errorf("sync: mark postings seen: %w", err)
	}
	for _, existing := range existingPostings {
		if existing.ListingStatus != "open" || seenSourceIDs[existing.SourceID] {
			continue
		}
		closed, err := s.store.ClosePosting(ctx, existing.ID, earlyApplicationStatuses)
		if err != nil {
			return result, fmt.Errorf("sync: close posting: %w", err)
		}
		if closed.Closed {
			result.Closed++
		}
		if closed.ApplicationClosed {
			result.ApplicationsClosed++
		}
	}

	if err := s.store.MarkCompanyFetched(ctx, companyID); err != nil {
		return result, fmt.Errorf("sync: mark company fetched: %w", err)
	}
	return result, nil
}

// CreateCompany creates a company after confirming sourceRef resolves to
// a real board on source's API (issue #36), so a typo'd slug is rejected
// up front instead of surfacing later as a fetch error, or silently as
// zero postings. It fails closed: any fetch error, including a timeout,
// blocks the create. The fetched postings are discarded; ingestion waits
// for the next sync, after the user has had a chance to set filters.
// Shared by the TUI's add-company form and ImportCompanies.
func (s *Syncer) CreateCompany(ctx context.Context, name, source, sourceRef string) (store.Company, error) {
	fetcher, ok := s.fetchers[source]
	if !ok {
		return store.Company{}, fmt.Errorf("sync: unsupported source %q", source)
	}
	if _, err := s.fetch(ctx, fetcher, sourceRef); err != nil {
		return store.Company{}, fmt.Errorf("sync: %s/%s does not resolve to a real board: %w", source, sourceRef, err)
	}
	company, err := s.store.CreateCompany(ctx, name, source, sourceRef)
	if err != nil {
		return store.Company{}, fmt.Errorf("sync: create company: %w", err)
	}
	return company, nil
}

// ImportResult reports the outcome of importing one seed.Entry.
// Company is the zero value when Err is set.
type ImportResult struct {
	Name      string
	Source    string
	SourceRef string
	Company   store.Company
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
// store.CreateCompany is already idempotent on (source, source_ref), so
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
			company, err = s.store.UpdateCompanyDescription(ctx, company.ID, e.Description)
			if err != nil {
				results[i].Err = fmt.Errorf("sync: set company description: %w", err)
				continue
			}
		}
		results[i].Company = company
	}
	return results
}

// earlyApplicationStatuses are the statuses from which a posting being
// taken down ends the application. Deliberately not every non-terminal
// status: a company routinely pulls a listing while still interviewing
// the candidates already in its pipeline, so an application at
// interviewing or beyond is a live process that the syncer must not
// overwrite (see issue #105 and decisions.log). Those are left alone.
// So is a submitted one (#174): a listing often comes down once the
// company has enough candidates, and the ones who applied are still
// being reviewed. Only an application never sent can no longer be.
//
// Closing a posting's application, and undoing that when the posting
// reappears (store.ReopenPosting, #174), are the only places sync reaches
// past postings and posting history into application state, a deliberate
// widening of what a sync does (see decisions.log). The policy stays
// here; store.ClosePosting applies it in the same transaction that
// closes the posting (#147).
var earlyApplicationStatuses = []store.ApplicationStatus{
	store.ApplicationStatusStarted,
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
	Company  store.Company
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
func (s *Syncer) AddCompany(ctx context.Context, name, source, sourceRef, description string) (AddCompanyResult, error) {
	existing, err := s.store.GetCompanyBySourceRef(ctx, source, sourceRef)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// New company: fall through to the board check and create.
	case err != nil:
		return AddCompanyResult{}, fmt.Errorf("sync: look up company: %w", err)
	case existing.DeletedAt != nil:
		return AddCompanyResult{Outcome: AddCompanySkippedDeleted, Company: existing}, nil
	default:
		if existing.Description == "" && description != "" {
			existing, err = s.store.UpdateCompanyDescription(ctx, existing.ID, description)
			if err != nil {
				return AddCompanyResult{}, fmt.Errorf("sync: set company description: %w", err)
			}
		}
		return AddCompanyResult{Outcome: AddCompanyAlreadyExists, Company: existing}, nil
	}

	fetcher, ok := s.fetchers[source]
	if !ok {
		return AddCompanyResult{}, fmt.Errorf("sync: unsupported source %q", source)
	}
	postings, err := s.fetch(ctx, fetcher, sourceRef)
	if err != nil {
		return AddCompanyResult{}, fmt.Errorf("sync: %s/%s does not resolve to a real board: %w", source, sourceRef, err)
	}

	company, err := s.store.CreateCompany(ctx, name, source, sourceRef)
	if err != nil {
		return AddCompanyResult{}, fmt.Errorf("sync: create company: %w", err)
	}
	company, err = s.store.UpdateCompanyDescription(ctx, company.ID, description)
	if err != nil {
		return AddCompanyResult{}, fmt.Errorf("sync: set company description: %w", err)
	}
	return AddCompanyResult{Outcome: AddCompanyCreated, Company: company, OpenJobs: len(postings)}, nil
}
