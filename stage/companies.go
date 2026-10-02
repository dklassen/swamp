package stage

import (
	"context"
	"fmt"
)

// CompanySummary is one company in Companies: what the agent needs to
// match a company the user names and pass its ID to search_postings.
type CompanySummary struct {
	ID     int64  `json:"ID"`
	Name   string `json:"Name"`
	Source string `json:"Source"`
	// OpenPostings counts open, unarchived postings, as the TUI's company
	// list does.
	OpenPostings int `json:"OpenPostings"`
}

// Companies returns every company the user hasn't deleted, ordered by
// name (#220, RFC 0006). About 45 today, so it isn't paged.
func (st *Stage) Companies(ctx context.Context) ([]CompanySummary, error) {
	companies, err := st.store.ListActiveCompanies(ctx)
	if err != nil {
		return nil, fmt.Errorf("stage: list companies: %w", err)
	}
	open, err := st.store.CountOpenPostingsByCompany(ctx)
	if err != nil {
		return nil, fmt.Errorf("stage: count open postings: %w", err)
	}
	summaries := make([]CompanySummary, len(companies))
	for i, c := range companies {
		summaries[i] = CompanySummary{ID: c.ID, Name: c.Name, Source: c.Source, OpenPostings: open[c.ID]}
	}
	return summaries, nil
}
