package sync

import (
	"context"

	"github.com/dklassen/swamp/greenhouse"
)

// GreenhouseFetcher adapts *greenhouse.Client into a PostingFetcher,
// translating greenhouse.Posting into sync's source-agnostic Posting.
// Team, EmploymentType, and WorkplaceType are left empty -- Greenhouse's
// job board API doesn't expose them.
type GreenhouseFetcher struct {
	Client *greenhouse.Client
}

func NewGreenhouseFetcher(client *greenhouse.Client) GreenhouseFetcher {
	return GreenhouseFetcher{Client: client}
}

func (f GreenhouseFetcher) FetchPostings(ctx context.Context, boardToken string) ([]Posting, error) {
	raw, err := f.Client.FetchPostings(ctx, boardToken)
	if err != nil {
		return nil, err
	}
	postings := make([]Posting, len(raw))
	for i, p := range raw {
		postings[i] = Posting{
			SourceID:        p.SourceID,
			Title:           p.Title,
			Department:      p.Department,
			Location:        p.Location,
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
