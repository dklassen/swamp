package stage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store"
)

// FullPosting is one posting with everything the agent reads to judge
// it: the summary, the description as text, its company's description,
// and the content hash a score of it must name.
type FullPosting struct {
	ID                 int64              `json:"ID"`
	Title              string             `json:"Title"`
	CompanyName        string             `json:"CompanyName"`
	CompanyDescription string             `json:"CompanyDescription"`
	Department         string             `json:"Department"`
	Location           string             `json:"Location"`
	WorkplaceType      string             `json:"WorkplaceType"`
	ListingStatus      string             `json:"ListingStatus"`
	PublishedAt        store.OptionalTime `json:"PublishedAt"`
	FirstSeenAt        time.Time          `json:"FirstSeenAt"`
	DescriptionText    string             `json:"DescriptionText"`
	JobURL             string             `json:"JobURL"`
	ApplicationURL     string             `json:"ApplicationURL"`
	Version            string             `json:"Version"`
}

// ReadPosting returns posting id in full. A posting at a company the user
// deleted is refused, as search leaves it out.
func (st *Stage) ReadPosting(ctx context.Context, id int64) (FullPosting, error) {
	posting, err := st.store.GetPosting(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return FullPosting{}, fmt.Errorf("stage: no posting %d: %w", id, err)
	}
	if err != nil {
		return FullPosting{}, fmt.Errorf("stage: get posting %d: %w", id, err)
	}
	company, err := st.store.GetCompanyIncludingDeleted(ctx, posting.CompanyID)
	if err != nil {
		return FullPosting{}, fmt.Errorf("stage: get company: %w", err)
	}
	if company.DeletedAt != nil {
		return FullPosting{}, fmt.Errorf("stage: posting %d is at %s, which was deleted", id, company.Name)
	}
	return FullPosting{
		ID:                 posting.ID,
		Title:              posting.Title,
		CompanyName:        company.Name,
		CompanyDescription: company.Description,
		Department:         posting.Department,
		Location:           posting.Location,
		WorkplaceType:      posting.WorkplaceType,
		ListingStatus:      posting.ListingStatus,
		PublishedAt:        posting.PublishedAt,
		FirstSeenAt:        posting.FirstSeenAt,
		DescriptionText:    posting.DescriptionText,
		JobURL:             posting.JobURL,
		ApplicationURL:     posting.ApplicationURL,
		Version:            posting.ContentSHA256,
	}, nil
}
