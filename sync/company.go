package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/store"
)

func toFilterRules(filters []store.CompanyFilter) []filter.Filter {
	rules := make([]filter.Filter, len(filters))
	for i, f := range filters {
		rules[i] = filter.Filter{Field: f.Field, Value: f.Value}
	}
	return rules
}

func toFilterPosting(p Posting) filter.Posting {
	return filter.Posting{Department: p.Department, Location: p.Location}
}

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toCreatePostingParams(companyID int64, source string, p Posting) store.CreatePostingParams {
	params := store.CreatePostingParams{
		CompanyID:       companyID,
		Source:          source,
		SourceID:        p.SourceID,
		Title:           p.Title,
		Department:      stringOrNil(p.Department),
		Team:            stringOrNil(p.Team),
		Location:        stringOrNil(p.Location),
		EmploymentType:  stringOrNil(p.EmploymentType),
		WorkplaceType:   stringOrNil(p.WorkplaceType),
		DescriptionHTML: stringOrNil(p.DescriptionHTML),
		DescriptionText: stringOrNil(p.DescriptionText),
		JobURL:          stringOrNil(p.JobURL),
		ApplicationURL:  stringOrNil(p.ApplicationURL),
		RawPayload:      string(p.RawPayload),
	}
	if !p.PublishedAt.IsZero() {
		t := p.PublishedAt
		params.PublishedAt = &t
	}
	return params
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// postingContentChanged reports whether any ingested field on existing
// differs from the freshly-fetched params.
func postingContentChanged(existing store.Posting, params store.CreatePostingParams) bool {
	return existing.Title != params.Title ||
		!stringPtrEqual(existing.Department, params.Department) ||
		!stringPtrEqual(existing.Team, params.Team) ||
		!stringPtrEqual(existing.Location, params.Location) ||
		!stringPtrEqual(existing.EmploymentType, params.EmploymentType) ||
		!stringPtrEqual(existing.WorkplaceType, params.WorkplaceType) ||
		!stringPtrEqual(existing.DescriptionHTML, params.DescriptionHTML) ||
		!stringPtrEqual(existing.DescriptionText, params.DescriptionText) ||
		!stringPtrEqual(existing.JobURL, params.JobURL) ||
		!stringPtrEqual(existing.ApplicationURL, params.ApplicationURL) ||
		!timePtrEqual(existing.PublishedAt, params.PublishedAt) ||
		existing.RawPayload != params.RawPayload
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

	filters, err := s.store.ListCompanyFilters(ctx, companyID)
	if err != nil {
		return result, fmt.Errorf("sync: list company filters: %w", err)
	}
	rules := toFilterRules(filters)

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
