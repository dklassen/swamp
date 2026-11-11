package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/jobboard"
)

// candidate is one posting in list_postings: a summary, enough to pick one
// (stage_prepare returns its content for drafting), plus its application,
// if it has one.
type candidate struct {
	Posting           postingSummary                 `json:"Posting"`
	CompanyName       string                         `json:"CompanyName"`
	ApplicationID     *int64                         `json:"ApplicationID"`
	ApplicationStatus *application.Status            `json:"ApplicationStatus"`
	ApplicationNotes  string                         `json:"ApplicationNotes"`
	LatestReviews     map[document.Type]latestReview `json:"LatestReviews"`
}

// postingSummary leaves out the description and raw payload, which made
// list_postings too large for agent clients.
type postingSummary struct {
	ID             int64  `json:"ID"`
	Title          string `json:"Title"`
	Department     string `json:"Department"`
	Location       string `json:"Location"`
	WorkplaceType  string `json:"WorkplaceType"`
	ApplicationURL string `json:"ApplicationURL"`
	// PublishedAt is null when the board didn't say.
	PublishedAt posting.OptionalTime `json:"PublishedAt"`
	FirstSeenAt time.Time            `json:"FirstSeenAt"`
}

func summaryOf(p posting.Posting) postingSummary {
	return postingSummary{
		ID:             p.ID,
		Title:          p.Title,
		Department:     p.Department,
		Location:       p.Location,
		WorkplaceType:  p.WorkplaceType,
		ApplicationURL: p.ApplicationURL,
		PublishedAt:    p.PublishedAt,
		FirstSeenAt:    p.FirstSeenAt,
	}
}

// preparedPosting leaves out RawPayload and DescriptionHTML: drafting reads
// DescriptionText, and those two were most of the response.
type preparedPosting struct {
	ID              int64                 `json:"ID"`
	CompanyID       int64                 `json:"CompanyID"`
	Source          string                `json:"Source"`
	SourceID        string                `json:"SourceID"`
	Title           string                `json:"Title"`
	Department      string                `json:"Department"`
	Team            string                `json:"Team"`
	Location        string                `json:"Location"`
	EmploymentType  string                `json:"EmploymentType"`
	WorkplaceType   string                `json:"WorkplaceType"`
	DescriptionText string                `json:"DescriptionText"`
	JobURL          string                `json:"JobURL"`
	ApplicationURL  string                `json:"ApplicationURL"`
	PublishedAt     posting.OptionalTime  `json:"PublishedAt"`
	ListingStatus   posting.ListingStatus `json:"ListingStatus"`
	FirstSeenAt     time.Time             `json:"FirstSeenAt"`
	LastSeenAt      time.Time             `json:"LastSeenAt"`
	CreatedAt       time.Time             `json:"CreatedAt"`
	UpdatedAt       time.Time             `json:"UpdatedAt"`
}

func preparedPostingOf(p posting.Posting) preparedPosting {
	return preparedPosting{
		ID:              p.ID,
		CompanyID:       p.CompanyID,
		Source:          p.Source,
		SourceID:        p.SourceID,
		Title:           p.Title,
		Department:      p.Department,
		Team:            p.Team,
		Location:        p.Location,
		EmploymentType:  p.EmploymentType,
		WorkplaceType:   p.WorkplaceType,
		DescriptionText: p.DescriptionText,
		JobURL:          p.JobURL,
		ApplicationURL:  p.ApplicationURL,
		PublishedAt:     p.PublishedAt,
		ListingStatus:   p.ListingStatus,
		FirstSeenAt:     p.FirstSeenAt,
		LastSeenAt:      p.LastSeenAt,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// preparedDocument's Version is what write_document takes back as
// ReplacesVersion.
type preparedDocument struct {
	Path    string `json:"Path"`
	Exists  bool   `json:"Exists"`
	Version string `json:"Version"`
}

// latestReview leaves out the review's content snapshot: the agent works
// from the document on disk.
type latestReview struct {
	Outcome   document.Outcome `json:"Outcome"`
	Notes     string           `json:"Notes"`
	Cycle     int64            `json:"Cycle"`
	CreatedAt time.Time        `json:"CreatedAt"`
}

func latestReviewsOf(reviews map[document.Type]document.Review) map[document.Type]latestReview {
	out := make(map[document.Type]latestReview, len(reviews))
	for documentType, r := range reviews {
		out[documentType] = latestReview{Outcome: r.Outcome, Notes: r.Notes, Cycle: r.Cycle, CreatedAt: r.CreatedAt}
	}
	return out
}

// stagePrepareOutput is stage_prepare's result. Documents is keyed by type
// name, so a new type reaches the agent without a new field.
type stagePrepareOutput struct {
	Posting          preparedPosting                    `json:"Posting"`
	CompanyName      string                             `json:"CompanyName"`
	ApplicationID    int64                              `json:"ApplicationID"`
	Documents        map[document.Type]preparedDocument `json:"Documents"`
	ApplicationNotes string                             `json:"ApplicationNotes"`
	LatestReviews    map[document.Type]latestReview     `json:"LatestReviews"`
	// ApplicationForm is null when none is stored; the agent then drafts
	// both documents.
	ApplicationForm *jobboard.ApplicationForm `json:"ApplicationForm"`
}

type listPostingsInput struct{}

// listPostingsOutput wraps the list because the MCP spec requires a tool's
// structuredContent to be a JSON object.
type listPostingsOutput struct {
	Postings []candidate
}

// Out is 'any', skipping schema inference, which panics on posting.OptionalTime:
// it embeds time.Time, whose schema is a string where inference wants an
// embedded struct's to be an object. Enums that marshal as names, such as
// application.Status, would be advertised as integers besides.
func listPostingsHandler(drafting draftingService) mcp.ToolHandlerFor[listPostingsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ listPostingsInput) (*mcp.CallToolResult, any, error) {
		queue, err := drafting.Queue(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("list_postings: %w", err)
		}
		candidates := make([]candidate, len(queue))
		for i, q := range queue {
			candidates[i] = candidate{Posting: summaryOf(q.Posting), CompanyName: q.CompanyName, LatestReviews: latestReviewsOf(q.LatestReviews)}
			if q.Application != nil {
				id, status := q.Application.ID, q.Application.Status
				candidates[i].ApplicationID, candidates[i].ApplicationStatus = &id, &status
				candidates[i].ApplicationNotes = q.Application.Notes
			}
		}
		return nil, listPostingsOutput{Postings: candidates}, nil
	}
}

type stagePrepareInput struct {
	PostingID int64 `json:"PostingID" jsonschema:"the posting's id, from list_postings' Posting.ID field"`
}

// Out is 'any' for the reason listPostingsHandler gives.
func stagePrepareHandler(drafting draftingService) mcp.ToolHandlerFor[stagePrepareInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in stagePrepareInput) (*mcp.CallToolResult, any, error) {
		p, err := drafting.Prepare(ctx, in.PostingID)
		if err != nil {
			return nil, nil, fmt.Errorf("stage_prepare: %w", err)
		}
		documents := make(map[document.Type]preparedDocument, len(p.Documents))
		for documentType, doc := range p.Documents {
			documents[documentType] = preparedDocument{Path: doc.Path, Exists: doc.Exists, Version: doc.Version}
		}
		return nil, stagePrepareOutput{
			Posting:          preparedPostingOf(p.Posting),
			CompanyName:      p.Company.Name,
			ApplicationID:    p.Application.ID,
			Documents:        documents,
			ApplicationNotes: p.Application.Notes,
			LatestReviews:    latestReviewsOf(p.LatestReviews),
			ApplicationForm:  p.Form,
		}, nil
	}
}
