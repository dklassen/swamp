package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/cursor"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/posting"
)

// The page sizes of search_postings and list_pending_postings: when the
// caller doesn't say, and the most ever returned.
const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// pageSize is limit, defaulted and capped.
func pageSize(limit int) int {
	if limit <= 0 {
		return defaultPageSize
	}
	return min(limit, maxPageSize)
}

// anyListingStatus matches open and closed postings alike.
const anyListingStatus = "any"

// sortOrder is one way search_postings can order its matches, and what the
// agent is told about it, including how stable it is to page through.
type sortOrder struct {
	Order       posting.Order
	Description string
}

// Name is what the agent passes as Sort.
func (o sortOrder) Name() string { return o.Order.String() }

// sortOrders is the registry of sort orders, default first. Adding one is
// an entry here plus its store query.
var sortOrders = []sortOrder{
	{posting.OrderIDAsc, "the order Swamp first saw the postings in (the default). Stable while paging: postings added meanwhile land on later pages."},
	{posting.OrderIDDesc, "newest to Swamp first. Stable while paging; postings added meanwhile sort ahead of the first page, so they aren't seen until you start again."},
	{posting.OrderPublishedDesc, "newest on the board first (by PublishedAt; postings with no publish date last). Stable while paging, unless a board changes a posting's publish date meanwhile: that one posting may then be missed or seen twice."},
	{posting.OrderScoreDesc, "best fit first (by Score, the current fit score; among equal scores, newest to Swamp first; unscored postings last). Stable while paging, unless a posting is scored meanwhile: that one posting may then be missed or seen twice."},
}

// companySummary is what the agent needs to match a company the user names
// and pass its ID to search_postings.
type companySummary struct {
	ID     int64  `json:"ID"`
	Name   string `json:"Name"`
	Source string `json:"Source"`
	// OpenPostings counts open, unarchived postings the company's filters
	// let through, as the TUI's company list does.
	OpenPostings int `json:"OpenPostings"`
}

type listCompaniesInput struct{}

type listCompaniesOutput struct {
	Companies []companySummary
}

// listCompaniesHandler isn't paged: there are about 45 companies.
func listCompaniesHandler(companies companyService) mcp.ToolHandlerFor[listCompaniesInput, listCompaniesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ listCompaniesInput) (*mcp.CallToolResult, listCompaniesOutput, error) {
		stats, err := companies.ListActiveWithOpenPostings(ctx)
		if err != nil {
			return nil, listCompaniesOutput{}, fmt.Errorf("list_companies: %w", err)
		}
		summaries := make([]companySummary, len(stats))
		for i, c := range stats {
			summaries[i] = companySummary{ID: c.ID, Name: c.Name, Source: c.Source, OpenPostings: c.OpenPostings}
		}
		return nil, listCompaniesOutput{Companies: summaries}, nil
	}
}

type searchPostingsInput struct {
	CompanyID           int64    `json:"CompanyID,omitempty" jsonschema:"one company's ID, from list_companies"`
	HasApplication      *bool    `json:"HasApplication,omitempty" jsonschema:"true: only postings with an application; false: only postings without one"`
	ApplicationStatuses []string `json:"ApplicationStatuses,omitempty" jsonschema:"only applications at one of these statuses"`
	Interested          *bool    `json:"Interested,omitempty" jsonschema:"true: only postings marked interested; false: only postings not marked"`
	ListingStatus       string   `json:"ListingStatus,omitempty" jsonschema:"open (the default), closed or any"`
	IncludeArchived     bool     `json:"IncludeArchived,omitempty" jsonschema:"include postings the user archived"`
	Sort                string   `json:"Sort,omitempty"` // enum and description from sortOrders, in searchPostingsInputSchema
	Cursor              string   `json:"Cursor,omitempty" jsonschema:"the previous page's NextCursor, unchanged; omit for the first page"`
	Query               string   `json:"Query,omitempty" jsonschema:"space-separated words, each of which must appear in the title, company, department or location, ignoring case"`
	Limit               int      `json:"Limit,omitempty" jsonschema:"postings per page: 50 by default, at most 100"`
}

// searchPostingsInputSchema infers search_postings' input schema and
// advertises its controlled values as enums, so the agent learns them
// from the schema instead of guessing. The SDK validates arguments
// against it before the handler runs. Panics like mcp.AddTool on an
// uninferrable schema: a programming error caught at startup.
func searchPostingsInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[searchPostingsInput](nil)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: infer search_postings input schema: %v", err))
	}
	all := application.All()
	statuses := make([]any, len(all))
	for i, status := range all {
		statuses[i] = status.String()
	}
	schema.Properties["ApplicationStatuses"].Items.Enum = statuses
	schema.Properties["ListingStatus"].Enum = []any{posting.ListingOpen.String(), posting.ListingClosed.String(), anyListingStatus}
	sort := schema.Properties["Sort"]
	sort.Enum = make([]any, len(sortOrders))
	description := make([]string, len(sortOrders)+1)
	description[0] = "the order to page in, one of:"
	for i, order := range sortOrders {
		sort.Enum[i] = order.Name()
		description[i+1] = order.Name() + ": " + order.Description
	}
	sort.Description = strings.Join(description, "\n")
	return schema
}

// searchResult is one page of search_postings' matches.
type searchResult struct {
	Postings []searchMatch `json:"Postings"`
	// NextCursor is nil on the last page.
	NextCursor *string `json:"NextCursor"`
	// Total is how many match in all when this page was read: a hint for
	// whether to narrow the search, not a promise about later pages.
	Total int `json:"Total"`
}

// searchMatch is list_postings' summary plus the posting's company,
// listing status, markup and application.
type searchMatch struct {
	Posting           postingSummary        `json:"Posting"`
	CompanyName       string                `json:"CompanyName"`
	ListingStatus     posting.ListingStatus `json:"ListingStatus"`
	Interested        bool                  `json:"Interested"`
	Archived          bool                  `json:"Archived"`
	ApplicationID     *int64                `json:"ApplicationID"`
	ApplicationStatus *application.Status   `json:"ApplicationStatus"`
	ApplicationNotes  string                `json:"ApplicationNotes"`
	// Score is 0 when the posting has none.
	Score       int    `json:"Score"`
	ScoreReason string `json:"ScoreReason"`
}

func searchMatchOf(l posting.Listing) searchMatch {
	match := searchMatch{
		Posting: postingSummary{
			ID:             l.ID,
			Title:          l.Title,
			Department:     l.Department,
			Location:       l.Location,
			WorkplaceType:  l.WorkplaceType,
			ApplicationURL: l.ApplicationURL,
			PublishedAt:    posting.OptionalTime{Time: l.PublishedAt},
			FirstSeenAt:    l.FirstSeenAt,
		},
		CompanyName:   l.CompanyName,
		ListingStatus: l.ListingStatus,
		Interested:    l.Interested,
		Archived:      l.Archived,
		Score:         l.Score,
		ScoreReason:   l.ScoreReason,
	}
	if l.ApplicationID != 0 {
		id, status := l.ApplicationID, l.ApplicationStatus
		match.ApplicationID, match.ApplicationStatus = &id, &status
		match.ApplicationNotes = l.ApplicationNotes
	}
	return match
}

// searchFilters is what a cursor is bound to: the filters after defaults,
// with statuses sorted and the query's terms lowered, so the same search
// fingerprints the same however it was written.
type searchFilters struct {
	CompanyID           int64
	HasApplication      *bool
	Interested          *bool
	ApplicationStatuses []string
	ListingStatus       string
	IncludeArchived     bool
	Terms               []string
}

// Out is 'any' for the reason listPostingsHandler gives.
func searchPostingsHandler(postings postingService) mcp.ToolHandlerFor[searchPostingsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in searchPostingsInput) (*mcp.CallToolResult, any, error) {
		result, err := searchPostings(ctx, postings, in)
		switch {
		case errors.Is(err, cursor.ErrMismatch):
			return nil, nil, errors.New("search_postings: this cursor belongs to a different search (other filters or sort); start again without a cursor")
		case errors.Is(err, cursor.ErrInvalid):
			return nil, nil, errors.New("search_postings: that isn't a cursor from search_postings; start again without a cursor")
		case err != nil:
			return nil, nil, fmt.Errorf("search_postings: %w", err)
		}
		return nil, result, nil
	}
}

func searchPostings(ctx context.Context, postings postingService, in searchPostingsInput) (*searchResult, error) {
	search := posting.Search{
		CompanyID:       in.CompanyID,
		IncludeArchived: in.IncludeArchived,
		HasApplication:  in.HasApplication,
		Interested:      in.Interested,
		Query:           in.Query,
		Limit:           pageSize(in.Limit),
	}
	filters := searchFilters{
		CompanyID:       in.CompanyID,
		HasApplication:  in.HasApplication,
		Interested:      in.Interested,
		ListingStatus:   in.ListingStatus,
		IncludeArchived: in.IncludeArchived,
	}
	if filters.ListingStatus == "" {
		filters.ListingStatus = posting.ListingOpen.String()
	}
	if filters.ListingStatus != anyListingStatus {
		status, err := posting.ParseListingStatus(filters.ListingStatus)
		if err != nil {
			return nil, fmt.Errorf("unknown listing status %q: want open, closed or any", in.ListingStatus)
		}
		search.ListingStatus = &status
	}
	sort := in.Sort
	if sort == "" {
		sort = sortOrders[0].Name()
	}
	index := slices.IndexFunc(sortOrders, func(o sortOrder) bool { return o.Name() == sort })
	if index < 0 {
		return nil, fmt.Errorf("unknown sort %q", in.Sort)
	}
	search.Order = sortOrders[index].Order
	for _, name := range in.ApplicationStatuses {
		status, err := application.ParseStatus(name)
		if err != nil {
			return nil, err
		}
		search.ApplicationStatuses = append(search.ApplicationStatuses, status)
		filters.ApplicationStatuses = append(filters.ApplicationStatuses, status.String())
	}
	slices.Sort(filters.ApplicationStatuses)
	for term := range strings.FieldsSeq(in.Query) {
		filters.Terms = append(filters.Terms, strings.ToLower(term))
	}
	fingerprint := cursor.Fingerprint(filters)
	if in.Cursor != "" {
		key, err := cursor.Decode(in.Cursor, sort, fingerprint)
		if err != nil {
			return nil, err
		}
		search.AfterID, search.AfterPublishedAt, search.AfterScore = key.ID, key.PublishedAt, key.Score
	}

	page, err := postings.Search(ctx, search)
	if err != nil {
		return nil, err
	}
	result := searchResult{Postings: make([]searchMatch, len(page.Listings)), Total: page.Total}
	for i, l := range page.Listings {
		result.Postings[i] = searchMatchOf(l)
	}
	if page.HasMore {
		last := page.Listings[len(page.Listings)-1]
		next := cursor.Encode(cursor.Key{Sort: sort, Filters: fingerprint, ID: last.ID, PublishedAt: last.PublishedAt, Score: last.Score})
		result.NextCursor = &next
	}
	return &result, nil
}
