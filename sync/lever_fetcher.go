package sync

import (
	"context"

	"github.com/dklassen/swamp/lever"
)

// LeverFetcher adapts *lever.Client into a PostingFetcher, translating
// lever.Posting into sync's source-agnostic Posting.
type LeverFetcher struct {
	Client *lever.Client
}

func NewLeverFetcher(client *lever.Client) LeverFetcher {
	return LeverFetcher{Client: client}
}

func (f LeverFetcher) FetchPostings(ctx context.Context, site string) ([]Posting, error) {
	raw, err := f.Client.FetchPostings(ctx, site)
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
