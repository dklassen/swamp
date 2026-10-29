package message

import "time"

// Company is what an event carries for a company. LastFetchedAt is zero
// until its postings are first fetched, and DeletedAt while it isn't
// deleted.
type Company struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Source        string    `json:"source"`
	SourceRef     string    `json:"source_ref"`
	Description   string    `json:"description"`
	LastFetchedAt time.Time `json:"last_fetched_at,omitzero"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	DeletedAt     time.Time `json:"deleted_at,omitzero"`
}

func (Company) Type() Type { return TypeCompany }

// CompanyList is the active companies, by name, and each one's open,
// unarchived posting count.
type CompanyList struct {
	Companies    []Company     `json:"companies"`
	OpenPostings map[int64]int `json:"open_postings"`
}

func (CompanyList) Type() Type { return TypeCompanyList }

// CompanyRow is one company as its list shows it.
type CompanyRow struct {
	Company      Company `json:"company"`
	OpenPostings int     `json:"open_postings"`
}

func (CompanyRow) Type() Type { return TypeCompanyRow }

// None is the body of a primitive that carries nothing, such as a pop.
type None struct{}

func (None) Type() Type { return TypeNone }

// SyncCompany asks for one company's postings to be fetched from its job
// board. The event's Subject is the company.
type SyncCompany struct {
	Company Company `json:"company"`
}

func (SyncCompany) Type() Type { return TypeSyncCompany }

// SyncAll starts a run that syncs Companies one after another, or stops
// the run under way.
type SyncAll struct {
	Companies []Company `json:"companies"`
}

func (SyncAll) Type() Type { return TypeSyncAll }

// SyncReport is one company's sync, the event's Subject. RunID is the
// sync-all run it was part of, 0 for a refresh. Code is zero when it
// synced; a run's step that didn't says why here rather than in an Error,
// which would take over the banner mid-run.
type SyncReport struct {
	RunID    int       `json:"run_id,omitzero"`
	Fetched  int       `json:"fetched"`
	Created  int       `json:"created"`
	Updated  int       `json:"updated"`
	Closed   int       `json:"closed"`
	Reopened int       `json:"reopened"`
	Code     ErrorCode `json:"code,omitzero"`
}

func (SyncReport) Type() Type { return TypeSyncReport }

// SyncAllReport is a run's totals once it has ended, finished or stopped:
// how many companies it reached, and by name those that failed or were
// already syncing elsewhere.
type SyncAllReport struct {
	Companies int      `json:"companies"`
	Failed    []string `json:"failed,omitempty"`
	Skipped   []string `json:"skipped,omitempty"`
}

func (SyncAllReport) Type() Type { return TypeSyncAllReport }

// ContentSize is the area a screen draws in: the terminal less the header
// and banner.
type ContentSize struct {
	Width int `json:"width"`
	Rows  int `json:"rows"`
}

func (ContentSize) Type() Type { return TypeContentSize }

// FocusChanged names the model now in focus, 0 when none is.
type FocusChanged struct {
	Focused ScreenInstance `json:"focused"`
}

func (FocusChanged) Type() Type { return TypeFocusChanged }

// Posting is what an event carries for a posting.
type Posting struct {
	ID              int64         `json:"id"`
	CompanyID       int64         `json:"company_id"`
	Title           string        `json:"title"`
	Department      string        `json:"department"`
	Team            string        `json:"team"`
	Location        string        `json:"location"`
	EmploymentType  string        `json:"employment_type"`
	WorkplaceType   string        `json:"workplace_type"`
	DescriptionText string        `json:"description_text"`
	JobURL          string        `json:"job_url"`
	ApplicationURL  string        `json:"application_url"`
	ListingStatus   ListingStatus `json:"listing_status"`
}

// Markup is how the user has marked a posting. A time is zero while the
// posting isn't marked that way. A change to it has the posting as its
// Subject, since markup is no entity of its own.
type Markup struct {
	PostingID    int64     `json:"posting_id"`
	InterestedAt time.Time `json:"interested_at,omitzero"`
	ArchivedAt   time.Time `json:"archived_at,omitzero"`
}

func (Markup) Type() Type { return TypeMarkup }

// PostingList is the open postings of the event's Subject, a company, that
// match its saved filters, each with its markup. Departments and Locations
// are those filters.
type PostingList struct {
	Postings    []Posting        `json:"postings"`
	Markup      map[int64]Markup `json:"markup"`
	Departments []string         `json:"departments,omitempty"`
	Locations   []string         `json:"locations,omitempty"`
}

func (PostingList) Type() Type { return TypePostingList }

// HideArchived is whether posting lists leave archived postings out.
type HideArchived struct {
	Hidden bool `json:"hidden"`
}

func (HideArchived) Type() Type { return TypeHideArchived }

// FilterSelection is the departments and locations a company's posting
// list keeps to.
type FilterSelection struct {
	Company     Company  `json:"company"`
	Departments []string `json:"departments,omitempty"`
	Locations   []string `json:"locations,omitempty"`
}

func (FilterSelection) Type() Type { return TypeFilterSelection }

// FilterOptions is the departments and locations the event's Subject, a
// company, has postings in.
type FilterOptions struct {
	Departments []string `json:"departments"`
	Locations   []string `json:"locations"`
}

func (FilterOptions) Type() Type { return TypeFilterOptions }
