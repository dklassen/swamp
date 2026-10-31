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
