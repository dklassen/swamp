package tui

import (
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/tui/message"
)

func companySnapshot(c company.Company) message.Company {
	s := message.Company{
		ID:            c.ID,
		Name:          c.Name,
		Source:        c.Source,
		SourceRef:     c.SourceRef,
		Description:   c.Description,
		LastFetchedAt: c.LastFetchedAt,
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}
	if c.DeletedAt != nil {
		s.DeletedAt = *c.DeletedAt
	}
	return s
}

// companyOf is the company a snapshot is of, for screens that still hold
// company.Company.
func companyOf(s message.Company) company.Company {
	c := company.Company{
		ID:            s.ID,
		Name:          s.Name,
		Source:        s.Source,
		SourceRef:     s.SourceRef,
		Description:   s.Description,
		LastFetchedAt: s.LastFetchedAt,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
	if !s.DeletedAt.IsZero() {
		c.DeletedAt = &s.DeletedAt
	}
	return c
}
