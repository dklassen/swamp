package stage

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/dklassen/swamp/store"
)

// Search's limits: how many matches it returns when the caller doesn't
// say, and the most it ever returns. Total still counts every match, so
// a caller that hits the cap knows to narrow the search (#215).
const (
	defaultSearchLimit = 25
	maxSearchLimit     = 100
)

// SearchOptions narrows Search. The zero value is every open,
// non-archived posting, up to defaultSearchLimit.
type SearchOptions struct {
	// Query's words must all appear, ignoring case, in the posting's
	// company, title, department or location.
	Query string
	// Company is a company name, ignoring case.
	Company string
	// HasApplication, when set, keeps only postings with (true) or
	// without (false) an application.
	HasApplication *bool
	// ApplicationStatuses, when non-empty, keeps only postings whose
	// application is at one of them.
	ApplicationStatuses []store.ApplicationStatus
	// Interested, when set, keeps only postings marked (true) or not
	// marked (false) interested.
	Interested *bool
	// ListingStatus is "open" (the default when empty), "closed" or "any".
	ListingStatus string
	// IncludeArchived includes postings the user archived.
	IncludeArchived bool
	// Limit caps the matches returned: defaultSearchLimit when 0 or
	// less, and never more than maxSearchLimit.
	Limit int
}

// SearchResult is Search's matches, and how many there were before Limit.
type SearchResult struct {
	Total    int           `json:"Total"`
	Postings []SearchMatch `json:"Postings"`
}

// SearchMatch is one posting Search found, in list_postings' Candidate
// shape plus the posting's listing status and markup, so the agent can
// tell the user which one is which.
type SearchMatch struct {
	Posting           PostingSummary           `json:"Posting"`
	CompanyName       string                   `json:"CompanyName"`
	ListingStatus     string                   `json:"ListingStatus"`
	Interested        bool                     `json:"Interested"`
	Archived          bool                     `json:"Archived"`
	ApplicationID     *int64                   `json:"ApplicationID"`
	ApplicationStatus *store.ApplicationStatus `json:"ApplicationStatus"`
	ApplicationNotes  string                   `json:"ApplicationNotes"`
}

// Search finds postings across every company the user hasn't deleted,
// with or without an application (#215): how the agent finds a posting or
// application the user names in their own words, which list_postings'
// drafting queue often doesn't include.
func (st *Stage) Search(ctx context.Context, opts SearchOptions) (SearchResult, error) {
	listingStatus := opts.ListingStatus
	switch listingStatus {
	case "":
		listingStatus = "open"
	case "open", "closed", "any":
	default:
		return SearchResult{}, fmt.Errorf("stage: unknown listing status %q: want open, closed or any", opts.ListingStatus)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	limit = min(limit, maxSearchLimit)
	words := strings.Fields(strings.ToLower(opts.Query))

	listings, err := st.store.ListPostingListings(ctx)
	if err != nil {
		return SearchResult{}, fmt.Errorf("stage: %w", err)
	}
	result := SearchResult{Postings: []SearchMatch{}}
	for _, l := range listings {
		hasApplication := l.ApplicationID != 0
		switch {
		case listingStatus != "any" && l.ListingStatus != listingStatus,
			l.Archived && !opts.IncludeArchived,
			opts.Company != "" && !strings.EqualFold(l.CompanyName, opts.Company),
			opts.HasApplication != nil && *opts.HasApplication != hasApplication,
			opts.Interested != nil && *opts.Interested != l.Interested,
			len(opts.ApplicationStatuses) > 0 && (!hasApplication || !slices.Contains(opts.ApplicationStatuses, l.ApplicationStatus)),
			!matchesAll(words, l):
			continue
		}
		result.Total++
		if len(result.Postings) < limit {
			result.Postings = append(result.Postings, searchMatch(l))
		}
	}
	return result, nil
}

// matchesAll reports whether every word appears in l's company, title,
// department or location. words are already lowercase.
func matchesAll(words []string, l store.PostingListing) bool {
	text := strings.ToLower(strings.Join([]string{l.CompanyName, l.Title, l.Department, l.Location}, " "))
	for _, word := range words {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

func searchMatch(l store.PostingListing) SearchMatch {
	match := SearchMatch{
		Posting: PostingSummary{
			ID:             l.ID,
			Title:          l.Title,
			Department:     l.Department,
			Location:       l.Location,
			WorkplaceType:  l.WorkplaceType,
			ApplicationURL: l.ApplicationURL,
		},
		CompanyName:   l.CompanyName,
		ListingStatus: l.ListingStatus,
		Interested:    l.Interested,
		Archived:      l.Archived,
	}
	if l.ApplicationID != 0 {
		id, status := l.ApplicationID, l.ApplicationStatus
		match.ApplicationID = &id
		match.ApplicationStatus = &status
		match.ApplicationNotes = l.ApplicationNotes
	}
	return match
}
