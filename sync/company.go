package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/jobboard"
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
	}

	return result, nil
}
