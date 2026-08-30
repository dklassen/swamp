package sync

import (
	"context"

	"github.com/dklassen/swamp/ashby"
)

// AshbyFetcher adapts *ashby.Client into a PostingFetcher, translating
// ashby.Posting into sync's source-agnostic Posting.
type AshbyFetcher struct {
	Client *ashby.Client
}

func NewAshbyFetcher(client *ashby.Client) AshbyFetcher {
	return AshbyFetcher{Client: client}
}

func (f AshbyFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]Posting, error) {
	raw, err := f.Client.FetchPostings(ctx, boardSlug)
	if err != nil {
		return nil, err
	}
	postings := make([]Posting, len(raw))
	for i, p := range raw {
		postings[i] = Posting{
			SourceID:        p.SourceID,
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
			RawPayload:      p.RawPayload,
		}
	}
	return postings, nil
}
