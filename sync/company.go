package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/go-cmp/cmp"

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
		PublishedAt:     p.PublishedAt,
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

// postingContentChanged reports whether any ingested field on existing
// differs from the freshly-fetched params.
func postingContentChanged(existing store.Posting, params store.CreatePostingParams) bool {
	return !cmp.Equal(existing.IngestedFields, params.IngestedFields)
}

// recordHistory snapshots posting's current ingested state and writes it
// as a posting_history row before the caller changes posting.
func (s *Syncer) recordHistory(ctx context.Context, posting store.Posting, changeType string) error {
	snapshot, err := json.Marshal(posting)
	if err != nil {
		return fmt.Errorf("sync: marshal posting snapshot: %w", err)
	}
	if _, err := s.store.CreatePostingHistory(ctx, posting.ID, changeType, string(snapshot)); err != nil {
		return fmt.Errorf("sync: record posting history: %w", err)
	}
	return nil
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
func (s *Syncer) SyncCompany(ctx context.Context, companyID int64) (Result, error) {
	result := Result{CompanyID: companyID}

	company, err := s.store.GetCompany(ctx, companyID)
	if err != nil {
		return result, fmt.Errorf("sync: get company: %w", err)
	}
	fetcher, ok := s.fetchers[company.Source]
	if !ok {
		return result, fmt.Errorf("sync: unsupported source %q", company.Source)
	}

	fetched, err := fetcher.FetchPostings(ctx, company.SourceRef)
	if err != nil {
		return result, fmt.Errorf("sync: fetch postings: %w", err)
	}
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

		params := toCreatePostingParams(company.ID, company.Source, p)
		existing, err := s.store.GetPostingBySourceAndSourceID(ctx, company.Source, p.SourceID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if _, err := s.store.UpsertPosting(ctx, params); err != nil {
				return result, fmt.Errorf("sync: create posting: %w", err)
			}
			result.Created++
		case err != nil:
			return result, fmt.Errorf("sync: get existing posting: %w", err)
		default:
			if postingContentChanged(existing, params) {
				if err := s.recordHistory(ctx, existing, "content_updated"); err != nil {
					return result, err
				}
				if _, err := s.store.UpsertPosting(ctx, params); err != nil {
					return result, fmt.Errorf("sync: update posting: %w", err)
				}
				result.Updated++
			}
			if existing.ListingStatus == "closed" {
				if err := s.recordHistory(ctx, existing, "reopened"); err != nil {
					return result, err
				}
				if err := s.store.MarkPostingReopened(ctx, existing.ID); err != nil {
					return result, fmt.Errorf("sync: mark posting reopened: %w", err)
				}
				result.Reopened++
			}
		}
	}

	existingPostings, err := s.store.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		return result, fmt.Errorf("sync: list existing postings: %w", err)
	}
	for _, existing := range existingPostings {
		if existing.ListingStatus != "open" || seenSourceIDs[existing.SourceID] {
			continue
		}
		if err := s.recordHistory(ctx, existing, "closed"); err != nil {
			return result, err
		}
		if err := s.store.MarkPostingClosed(ctx, existing.ID); err != nil {
			return result, fmt.Errorf("sync: mark posting closed: %w", err)
		}
		result.Closed++
		closed, err := s.closeApplicationForClosedPosting(ctx, existing.ID)
		if err != nil {
			return result, err
		}
		if closed {
			result.ApplicationsClosed++
		}
	}

	return result, nil
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
// entry is validated against its source's real API before being saved
// (the same live-board check issue #36 specs for the manual add-company
// form, reused here rather than added a second time): an unsupported
// Source or a SourceRef that the board rejects fails that entry without
// creating a row, and without aborting the rest of the batch -- same
// per-item isolation as SyncAll. A successfully-fetched entry's postings
// are discarded, not ingested -- exactly like adding a company through
// the TUI, ingestion happens on the next fetch, not at creation time.
// store.CreateCompany is already idempotent on (source, source_ref), so
// re-running the same seed file is always safe.
func (s *Syncer) ImportCompanies(ctx context.Context, entries []seed.Entry) []ImportResult {
	results := make([]ImportResult, len(entries))
	for i, e := range entries {
		results[i] = ImportResult{Name: e.Name, Source: e.Source, SourceRef: e.SourceRef}

		fetcher, ok := s.fetchers[e.Source]
		if !ok {
			results[i].Err = fmt.Errorf("sync: unsupported source %q", e.Source)
			continue
		}
		if _, err := fetcher.FetchPostings(ctx, e.SourceRef); err != nil {
			results[i].Err = fmt.Errorf("sync: %s/%s does not resolve to a real board: %w", e.Source, e.SourceRef, err)
			continue
		}

		company, err := s.store.CreateCompany(ctx, e.Name, e.Source, e.SourceRef)
		if err != nil {
			results[i].Err = fmt.Errorf("sync: create company: %w", err)
			continue
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
var earlyApplicationStatuses = []store.ApplicationStatus{
	store.ApplicationStatusStarted,
	store.ApplicationStatusSubmitted,
}

// closeApplicationForClosedPosting moves postingID's application to
// posting_closed if it exists and is still at an early status, reporting
// whether it did. A posting with no application at all is the common
// case (most postings are never applied to) and is not an error.
//
// This is the one place sync reaches past postings and posting history
// into application state, which is a deliberate widening of what a sync
// does -- see decisions.log for why it lives here rather than being
// derived at read time.
func (s *Syncer) closeApplicationForClosedPosting(ctx context.Context, postingID int64) (bool, error) {
	application, err := s.store.GetApplication(ctx, postingID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("sync: get application for closed posting: %w", err)
	}

	if !slices.Contains(earlyApplicationStatuses, application.Status) {
		return false, nil
	}
	if _, err := s.store.UpdateApplicationStatus(ctx, postingID, store.ApplicationStatusPostingClosed); err != nil {
		return false, fmt.Errorf("sync: close application for closed posting: %w", err)
	}
	return true, nil
}
