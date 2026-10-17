package posting

import (
	"fmt"
	"time"

	"github.com/dklassen/swamp/internal/domain/application"
)

// Listing is one posting in a search's results: its summary fields, its
// company, its markup flags and its application, if any (RFC 0006). No
// description or raw payload (#117).
type Listing struct {
	ID             int64
	Title          string
	Department     string
	Location       string
	WorkplaceType  string
	JobURL         string
	ApplicationURL string
	// PublishedAt is when the board says the posting was published; zero
	// when it didn't say.
	PublishedAt time.Time
	// FirstSeenAt is when Swamp first stored the posting.
	FirstSeenAt   time.Time
	ListingStatus string
	CompanyName   string
	Interested    bool
	Archived      bool
	// Score and ScoreReason are the posting's current score, its newest;
	// Score is 0 when it has none, since scores start at 1.
	Score       int
	ScoreReason string
	// ScoreStale is whether the content has changed since it was scored.
	ScoreStale bool
	// ApplicationID is 0 when the posting has no application; the other
	// Application fields are meaningless then.
	ApplicationID     int64
	ApplicationStatus application.Status
	ApplicationNotes  string
}

// Search is one page's worth of a posting search. Every filter is
// optional: its zero value doesn't filter. Limit must be positive.
type Search struct {
	// CompanyID is one company's ID; 0 is any company.
	CompanyID int64
	// ListingStatus is "open" or "closed"; "" is either.
	ListingStatus   string
	IncludeArchived bool
	// HasApplication and Interested filter when set; nil is either.
	HasApplication      *bool
	Interested          *bool
	ApplicationStatuses []application.Status
	// Query is space-separated terms, each of which must appear in the
	// posting's title, company, department or location, ignoring case.
	// "" matches every posting.
	Query string
	// IgnoreCompanyFilters includes postings their company's filters
	// hide; by default a search shows what the company's list shows.
	IgnoreCompanyFilters bool
	// Order is the sort order; the zero value is OrderIDAsc.
	Order Order
	// AfterID is the last posting ID of the previous page: the next page
	// is the rows after it in Order. 0 starts from the beginning.
	AfterID int64
	// AfterPublishedAt is that row's PublishedAt, for OrderPublishedDesc;
	// zero when it had none. Other orders ignore it.
	AfterPublishedAt time.Time
	// AfterScore is that row's Score, for OrderScoreDesc; 0 when it had
	// none. Other orders ignore it.
	AfterScore int
	Limit      int
}

// Order is a sort order a search pages in (#223, RFC 0006). Each ends
// with the posting ID, which never changes, so paging stays stable; each
// is its own static query, with the same filters.
type Order int

const (
	// OrderIDAsc: the order Swamp first saw the postings in.
	OrderIDAsc Order = iota
	// OrderIDDesc: the postings Swamp saw most recently first.
	OrderIDDesc
	// OrderPublishedDesc: newest on the board first, by published_at then
	// ID, descending; postings with no published_at last. Stable while
	// paging unless a board changes a posting's published_at mid-paging:
	// that posting can cross the cursor.
	OrderPublishedDesc
	// OrderScoreDesc: best fit first, by current score, then newest to
	// Swamp among equal scores; unscored postings last.
	OrderScoreDesc
)

// orderNames is the single name table for Order, indexed by its value.
var orderNames = [...]string{
	OrderIDAsc:         "id_asc",
	OrderIDDesc:        "id_desc",
	OrderPublishedDesc: "published_desc",
	OrderScoreDesc:     "score_desc",
}

func (o Order) String() string {
	if o < 0 || int(o) >= len(orderNames) {
		return fmt.Sprintf("PostingOrder(%d)", int(o))
	}
	return orderNames[o]
}

// Orders returns every sort order, in const order.
func Orders() []Order {
	orders := make([]Order, len(orderNames))
	for i := range orderNames {
		orders[i] = Order(i)
	}
	return orders
}

// SearchPage is one page of a search's results.
type SearchPage struct {
	Listings []Listing
	// HasMore is whether postings after this page match too. Following it
	// never leads to an empty page past the end.
	HasMore bool
	// Total is how many postings match the search in all, when the page
	// was read. It's 0 on an empty page: nothing matches, or nothing after
	// AfterID does any more.
	Total int
}
