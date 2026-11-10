package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/cursor"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
)

// fullPosting is everything the agent reads to judge a posting, and the
// content hash a score of it must name.
type fullPosting struct {
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

type readPostingInput struct {
	PostingID int64 `json:"PostingID" jsonschema:"the posting's id, from search_postings' Posting.ID field"`
}

// Out is 'any' for the reason listPostingsHandler gives.
func readPostingHandler(postings postingService) mcp.ToolHandlerFor[readPostingInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in readPostingInput) (*mcp.CallToolResult, any, error) {
		p, c, err := postings.GetWithCompany(ctx, in.PostingID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("read_posting: no posting %d: %w", in.PostingID, err)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read_posting: %w", err)
		}
		return nil, fullPosting{
			ID:                 p.ID,
			Title:              p.Title,
			CompanyName:        c.Name,
			CompanyDescription: c.Description,
			Department:         p.Department,
			Location:           p.Location,
			WorkplaceType:      p.WorkplaceType,
			ListingStatus:      p.ListingStatus,
			PublishedAt:        p.PublishedAt,
			FirstSeenAt:        p.FirstSeenAt,
			DescriptionText:    p.DescriptionText,
			JobURL:             p.JobURL,
			ApplicationURL:     p.ApplicationURL,
			Version:            p.ContentSHA256,
		}, nil
	}
}

// pendingSort names list_pending_postings' one order in its cursors, so a
// search_postings cursor isn't taken for one.
const pendingSort = "pending"

type listPendingPostingsInput struct {
	ProfileVersion string `json:"ProfileVersion" jsonschema:"read_profile's Version"`
	Rescore        bool   `json:"Rescore,omitempty" jsonschema:"list postings with no score against this profile, not just none at all; only when the user asks for a rescore"`
	Cursor         string `json:"Cursor,omitempty" jsonschema:"the previous page's NextCursor, unchanged; omit for the first page"`
	Limit          int    `json:"Limit,omitempty" jsonschema:"postings per page: 50 by default, at most 100"`
}

// pendingResult is one page of pending postings.
type pendingResult struct {
	Postings []pendingMatch `json:"Postings"`
	// NextCursor is nil on the last page.
	NextCursor *string `json:"NextCursor"`
	// Total is how many are pending in all when this page was read.
	Total int `json:"Total"`
}

// pendingMatch is a posting still to score: enough to judge it from, the
// content hash its score must name, and why it's pending.
type pendingMatch struct {
	ID            int64                `json:"ID"`
	Title         string               `json:"Title"`
	CompanyName   string               `json:"CompanyName"`
	Department    string               `json:"Department"`
	Location      string               `json:"Location"`
	WorkplaceType string               `json:"WorkplaceType"`
	PublishedAt   posting.OptionalTime `json:"PublishedAt"`
	Version       string               `json:"Version"`
	Reason        score.PendingReason  `json:"Reason"`
}

// pendingFilters is what a list_pending_postings cursor is bound to.
type pendingFilters struct {
	Rescore        bool
	ProfileVersion string
}

// Out is 'any' for the reason listPostingsHandler gives.
func listPendingPostingsHandler(scores scoringService) mcp.ToolHandlerFor[listPendingPostingsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listPendingPostingsInput) (*mcp.CallToolResult, any, error) {
		filters := pendingFilters{Rescore: in.Rescore}
		if in.Rescore {
			filters.ProfileVersion = in.ProfileVersion
		}
		fingerprint := cursor.Fingerprint(filters)
		var beforeID int64
		if in.Cursor != "" {
			key, err := cursor.Decode(in.Cursor, pendingSort, fingerprint)
			if err != nil {
				return nil, nil, fmt.Errorf("list_pending_postings: %w", err)
			}
			beforeID = key.ID
		}

		page, err := scores.Pending(ctx, in.ProfileVersion, in.Rescore, beforeID, pageSize(in.Limit))
		if err != nil {
			return nil, nil, scoringError("list_pending_postings", err)
		}
		result := pendingResult{Postings: make([]pendingMatch, len(page.Postings)), Total: page.Total}
		for i, p := range page.Postings {
			result.Postings[i] = pendingMatch{
				ID:            p.ID,
				Title:         p.Title,
				CompanyName:   p.CompanyName,
				Department:    p.Department,
				Location:      p.Location,
				WorkplaceType: p.WorkplaceType,
				PublishedAt:   posting.OptionalTime{Time: p.PublishedAt},
				Version:       p.ContentSHA256,
				Reason:        p.Reason,
			}
		}
		if page.HasMore {
			next := cursor.Encode(cursor.Key{Sort: pendingSort, Filters: fingerprint, ID: page.Postings[len(page.Postings)-1].ID})
			result.NextCursor = &next
		}
		return nil, result, nil
	}
}

// scoringError says what to do about a profile the scores can't be judged
// against.
func scoringError(tool string, err error) error {
	var missing *service.NoProfileError
	switch {
	case errors.As(err, &missing):
		return fmt.Errorf("%s: there's no profile at %s to score against", tool, missing.Path)
	case errors.Is(err, service.ErrProfileChanged):
		return fmt.Errorf("%s: the profile isn't the version you gave (it changed, or wasn't read): call read_profile and pass its Version", tool)
	}
	return fmt.Errorf("%s: %w", tool, err)
}

// scoreInput is one posting's score, as the agent gives it.
type scoreInput struct {
	PostingID      int64  `json:"PostingID"`
	Score          int    `json:"Score"`
	Reason         string `json:"Reason"`
	PostingVersion string `json:"PostingVersion"`
}

type recordScoresInput struct {
	Model          string       `json:"Model" jsonschema:"your model ID, e.g. claude-opus-5-5"`
	ProfileVersion string       `json:"ProfileVersion" jsonschema:"read_profile's Version"`
	Scores         []scoreInput `json:"Scores" jsonschema:"the scores, each with PostingID, Score (1-5), Reason (one line) and, as PostingVersion, the posting's Version you read"`
}

// recordResult lists are in the order given, and empty rather than null.
type recordResult struct {
	Recorded []int64        `json:"Recorded"`
	Refused  []refusedEntry `json:"Refused"`
}

type refusedEntry struct {
	PostingID int64         `json:"PostingID"`
	Why       score.Refusal `json:"Why"`
}

// recordScoresOutputSchema advertises a refusal's Why as its name, one of
// score.Refusals(): inferred, it would be score.Refusal's int.
func recordScoresOutputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[recordResult](nil)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: infer record_scores output schema: %v", err))
	}
	refusals := score.Refusals()
	names := make([]any, len(refusals))
	for i, refusal := range refusals {
		names[i] = refusal.String()
	}
	schema.Properties["Refused"].Items.Properties["Why"] = &jsonschema.Schema{Type: "string", Enum: names}
	return schema
}

// recordScoresHandler records in one transaction; a stale profile hash
// records nothing.
func recordScoresHandler(scores scoringService) mcp.ToolHandlerFor[recordScoresInput, recordResult] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in recordScoresInput) (*mcp.CallToolResult, recordResult, error) {
		entries := make([]score.Entry, len(in.Scores))
		for i, s := range in.Scores {
			entries[i] = score.Entry{PostingID: s.PostingID, Score: s.Score, Reason: s.Reason, ContentSHA256: s.PostingVersion}
		}
		provenance := score.Provenance{Model: in.Model, ProfileSHA256: in.ProfileVersion}
		// Client info is optional in MCP; scores without it record none.
		if params := req.Session.InitializeParams(); params != nil && params.ClientInfo != nil {
			provenance.ClientName, provenance.ClientVersion = params.ClientInfo.Name, params.ClientInfo.Version
		}
		recorded, err := scores.Record(ctx, provenance, entries)
		if err != nil {
			return nil, recordResult{}, scoringError("record_scores", err)
		}
		result := recordResult{Recorded: append([]int64{}, recorded.Recorded...), Refused: make([]refusedEntry, len(recorded.Refused))}
		for i, r := range recorded.Refused {
			result.Refused[i] = refusedEntry{PostingID: r.PostingID, Why: r.Why}
		}
		return nil, result, nil
	}
}
