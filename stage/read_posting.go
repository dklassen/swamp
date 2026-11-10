package stage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/store"
)

// FullPosting is one posting with everything the agent reads to judge
// it: the summary, the description as text, its company's description,
// and the content hash a score of it must name.
type FullPosting struct {
	ID                 int64                 `json:"ID"`
	Title              string                `json:"Title"`
	CompanyName        string                `json:"CompanyName"`
	CompanyDescription string                `json:"CompanyDescription"`
	Department         string                `json:"Department"`
	Location           string                `json:"Location"`
	WorkplaceType      string                `json:"WorkplaceType"`
	ListingStatus      posting.ListingStatus `json:"ListingStatus"`
	PublishedAt        posting.OptionalTime  `json:"PublishedAt"`
	FirstSeenAt        time.Time             `json:"FirstSeenAt"`
	DescriptionText    string                `json:"DescriptionText"`
	JobURL             string                `json:"JobURL"`
	ApplicationURL     string                `json:"ApplicationURL"`
	Version            string                `json:"Version"`
}

// ReadPosting returns posting id in full, refusing one at a deleted
// company (service.CompanyDeletedError).
func (st *Stage) ReadPosting(ctx context.Context, id int64) (*FullPosting, error) {
	posting, company, err := st.postings.GetWithCompany(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("stage: no posting %d: %w", id, err)
	}
	if err != nil {
		return nil, fmt.Errorf("stage: %w", err)
	}
	return &FullPosting{
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
