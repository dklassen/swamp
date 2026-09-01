package sync

import "strings"

// sanitizePosting trims leading/trailing whitespace from every
// free-text field a source's API returns before it's used for filter
// matching or persisted. Job board data is inconsistent in practice --
// e.g. Stripe's Greenhouse listings have shown up as both "Dublin" and
// "Dublin " -- and filter.Match does an exact (case-insensitive)
// comparison, so untrimmed padding silently breaks matching in addition
// to leaving messy values in storage.
//
// RawPayload is deliberately left untouched: it's the source's raw JSON
// response, kept verbatim for audit/debugging, not a display field.
// PublishedAt isn't a string.
func sanitizePosting(p Posting) Posting {
	p.SourceID = strings.TrimSpace(p.SourceID)
	p.Title = strings.TrimSpace(p.Title)
	p.Department = strings.TrimSpace(p.Department)
	p.Team = strings.TrimSpace(p.Team)
	p.Location = strings.TrimSpace(p.Location)
	p.EmploymentType = strings.TrimSpace(p.EmploymentType)
	p.WorkplaceType = strings.TrimSpace(p.WorkplaceType)
	p.DescriptionHTML = strings.TrimSpace(p.DescriptionHTML)
	p.DescriptionText = strings.TrimSpace(p.DescriptionText)
	p.JobURL = strings.TrimSpace(p.JobURL)
	p.ApplicationURL = strings.TrimSpace(p.ApplicationURL)
	return p
}
