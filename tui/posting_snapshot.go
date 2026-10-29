package tui

import (
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/tui/message"
)

var listingStatuses = map[posting.ListingStatus]message.ListingStatus{
	posting.ListingOpen:   message.ListingOpen,
	posting.ListingClosed: message.ListingClosed,
}

func postingSnapshot(p posting.Posting) message.Posting {
	return message.Posting{
		ID:              p.ID,
		CompanyID:       p.CompanyID,
		Title:           p.Title,
		Department:      p.Department,
		Team:            p.Team,
		Location:        p.Location,
		EmploymentType:  p.EmploymentType,
		WorkplaceType:   p.WorkplaceType,
		DescriptionText: p.DescriptionText,
		JobURL:          p.JobURL,
		ApplicationURL:  p.ApplicationURL,
		ListingStatus:   listingStatuses[p.ListingStatus],
		ContentSHA256:   p.ContentSHA256,
	}
}

// postingOf is the posting a snapshot is of, for screens that still hold
// posting.Posting. It has only the fields the snapshot carries.
func postingOf(s message.Posting) posting.Posting {
	p := posting.Posting{
		ID:        s.ID,
		CompanyID: s.CompanyID,
		IngestedFields: posting.IngestedFields{
			Title:           s.Title,
			Department:      s.Department,
			Team:            s.Team,
			Location:        s.Location,
			EmploymentType:  s.EmploymentType,
			WorkplaceType:   s.WorkplaceType,
			DescriptionText: s.DescriptionText,
			JobURL:          s.JobURL,
			ApplicationURL:  s.ApplicationURL,
		},
		ContentSHA256: s.ContentSHA256,
	}
	for status, snapshot := range listingStatuses {
		if snapshot == s.ListingStatus {
			p.ListingStatus = status
		}
	}
	return p
}

func listingSnapshot(l posting.Listing) message.Listing {
	return message.Listing{
		ID:             l.ID,
		CompanyName:    l.CompanyName,
		Title:          l.Title,
		Department:     l.Department,
		Location:       l.Location,
		WorkplaceType:  l.WorkplaceType,
		JobURL:         l.JobURL,
		ApplicationURL: l.ApplicationURL,
		ListingStatus:  listingStatuses[l.ListingStatus],
		FirstSeenAt:    l.FirstSeenAt,
		Interested:     l.Interested,
		Archived:       l.Archived,
		Score:          l.Score,
		ScoreStale:     l.ScoreStale,
	}
}

// listingPosting is the posting a search row is of, with only the fields
// the row has.
func listingPosting(l message.Listing) message.Posting {
	return message.Posting{
		ID:             l.ID,
		Title:          l.Title,
		Department:     l.Department,
		Location:       l.Location,
		WorkplaceType:  l.WorkplaceType,
		JobURL:         l.JobURL,
		ApplicationURL: l.ApplicationURL,
		ListingStatus:  l.ListingStatus,
	}
}

func markupSnapshot(m posting.Markup) message.Markup {
	s := message.Markup{PostingID: m.PostingID}
	if m.InterestedAt != nil {
		s.InterestedAt = *m.InterestedAt
	}
	if m.ArchivedAt != nil {
		s.ArchivedAt = *m.ArchivedAt
	}
	return s
}
