package message

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

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
	ContentSHA256   string        `json:"content_sha256"`
}

func (Posting) Type() Type { return TypePosting }

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

// Application is what an event carries for an application. DeletedAt is
// zero while it isn't deleted.
type Application struct {
	ID        int64             `json:"id"`
	PostingID int64             `json:"posting_id"`
	Status    ApplicationStatus `json:"status"`
	Notes     string            `json:"notes"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	DeletedAt time.Time         `json:"deleted_at,omitzero"`
}

func (Application) Type() Type { return TypeApplication }

// PostingDetail is what posting detail opens on: the posting, its
// application, zero when it has none, and the postings it steps through,
// by ID in list order, empty when it doesn't step.
type PostingDetail struct {
	Posting     Posting     `json:"posting"`
	Application Application `json:"application,omitzero"`
	Siblings    []int64     `json:"siblings,omitempty"`
}

func (PostingDetail) Type() Type { return TypePostingDetail }

// PostingApplication is the event's Subject, a posting, and its
// application, zero when it has none.
type PostingApplication struct {
	Posting     Posting     `json:"posting"`
	Application Application `json:"application,omitzero"`
}

func (PostingApplication) Type() Type { return TypePostingApplication }

// Review is what an event carries for a document review.
type Review struct {
	ID            int64         `json:"id"`
	ApplicationID int64         `json:"application_id"`
	DocumentType  DocumentType  `json:"document_type"`
	Outcome       ReviewOutcome `json:"outcome"`
	Notes         string        `json:"notes"`
	CreatedAt     time.Time     `json:"created_at"`
}

func (Review) Type() Type { return TypeReview }

// DocumentWrite is a recorded write of one of an application's documents,
// ContentSHA256 what was written. A change to it has the application as
// its Subject.
type DocumentWrite struct {
	ID            int64        `json:"id"`
	ApplicationID int64        `json:"application_id"`
	DocumentType  DocumentType `json:"document_type"`
	ContentSHA256 string       `json:"content_sha256"`
	WrittenAt     time.Time    `json:"written_at"`
}

func (DocumentWrite) Type() Type { return TypeDocumentWrite }

// DocumentExport is a recorded export of one of an application's documents
// as a PDF, ContentSHA256 what was exported. A change to it has the
// application as its Subject.
type DocumentExport struct {
	ID            int64        `json:"id"`
	ApplicationID int64        `json:"application_id"`
	DocumentType  DocumentType `json:"document_type"`
	ContentSHA256 string       `json:"content_sha256"`
	ExportedAt    time.Time    `json:"exported_at"`
}

func (DocumentExport) Type() Type { return TypeDocumentExport }

// ReviewList is the latest review of each document of the event's Subject,
// an application, that still describes the document as it is. A document
// with none isn't in it.
type ReviewList struct {
	Reviews []Review `json:"reviews"`
}

func (ReviewList) Type() Type { return TypeReviewList }

// Score is one fit score given to a posting. ContentSHA256 is the posting
// content it scored. A change to it has the posting as its Subject, since
// the screens that show it show it by posting.
type Score struct {
	ID            int64     `json:"id"`
	PostingID     int64     `json:"posting_id"`
	Score         int       `json:"score"`
	Reason        string    `json:"reason"`
	Model         string    `json:"model"`
	ClientName    string    `json:"client_name,omitempty"`
	ClientVersion string    `json:"client_version,omitempty"`
	ContentSHA256 string    `json:"content_sha256"`
	ScoredAt      time.Time `json:"scored_at"`
}

func (Score) Type() Type { return TypeScore }

// PostingScores is every score the event's Subject, a posting, was given,
// newest first.
type PostingScores struct {
	Scores []Score `json:"scores"`
}

func (PostingScores) Type() Type { return TypePostingScores }

// Listing is one posting as a search lists it: with its company's name,
// its markup and its current score, Score 0 when it has none.
type Listing struct {
	ID             int64         `json:"id"`
	CompanyName    string        `json:"company_name"`
	Title          string        `json:"title"`
	Department     string        `json:"department"`
	Location       string        `json:"location"`
	WorkplaceType  string        `json:"workplace_type"`
	JobURL         string        `json:"job_url"`
	ApplicationURL string        `json:"application_url"`
	ListingStatus  ListingStatus `json:"listing_status"`
	FirstSeenAt    time.Time     `json:"first_seen_at"`
	Interested     bool          `json:"interested"`
	Archived       bool          `json:"archived"`
	Score          int           `json:"score,omitzero"`
	// ScoreStale is whether the posting's content has changed since Score.
	ScoreStale bool `json:"score_stale,omitzero"`
}

// PostingSearch is a page of every company's open postings that its
// filters let through, and the search it's a page of: Query, in Order,
// after the row AfterID (and AfterScore, by fit), Limit rows at most.
// Total is how many match in all, and WithoutQuery how many would with no
// query.
type PostingSearch struct {
	Query        string       `json:"query,omitempty"`
	Order        PostingOrder `json:"order"`
	AfterID      int64        `json:"after_id,omitzero"`
	AfterScore   int          `json:"after_score,omitzero"`
	Limit        int          `json:"limit"`
	Listings     []Listing    `json:"listings"`
	HasMore      bool         `json:"has_more"`
	Total        int          `json:"total"`
	WithoutQuery int          `json:"without_query"`
}

func (PostingSearch) Type() Type { return TypePostingSearch }

// SearchDebounce is the pause after typing Query into a search.
type SearchDebounce struct {
	Query string `json:"query"`
}

func (SearchDebounce) Type() Type { return TypeSearchDebounce }

// OpenBrowser is a URL opened in the browser. The event's Subject is what
// the URL is of.
type OpenBrowser struct {
	URL string `json:"url"`
}

func (OpenBrowser) Type() Type { return TypeOpenBrowser }

// DocumentProgress is one of an application's documents as it stands on
// disk: whether it's drafted, and whether what's there now was exported.
type DocumentProgress struct {
	DocumentType DocumentType `json:"document_type"`
	Drafted      bool         `json:"drafted"`
	Exported     bool         `json:"exported"`
}

// ApplicationRow is one application as the home screen lists it: with its
// posting, its company's name, the latest review of each document that
// still describes it, when it took its status, and its documents'
// progress.
type ApplicationRow struct {
	Application Application        `json:"application"`
	Posting     Posting            `json:"posting"`
	CompanyName string             `json:"company_name"`
	Reviews     []Review           `json:"reviews,omitempty"`
	StatusSince time.Time          `json:"status_since,omitzero"`
	Documents   []DocumentProgress `json:"documents"`
}

// ActiveApplications is every application not at a dead end, in the order
// the home screen lists them.
type ActiveApplications struct {
	Rows []ApplicationRow `json:"rows"`
}

func (ActiveApplications) Type() Type { return TypeActiveApplications }

// ApplicationDetail is one application with its posting, its company's
// name, and the latest review of each document that still describes it.
type ApplicationDetail struct {
	Application Application `json:"application"`
	Posting     Posting     `json:"posting"`
	CompanyName string      `json:"company_name"`
	Reviews     []Review    `json:"reviews,omitempty"`
}

func (ApplicationDetail) Type() Type { return TypeApplicationDetail }

// EditDocument is an edit of one of an application's documents in the
// user's editor, which records what it wrote before it reports.
type EditDocument struct {
	DocumentType DocumentType `json:"document_type"`
}

func (EditDocument) Type() Type { return TypeEditDocument }

// DocumentReview is what a review form opens on: one of an application's
// documents, as Content read when it was picked, which is what a review
// saved from the form describes.
type DocumentReview struct {
	ApplicationID int64        `json:"application_id"`
	DocumentType  DocumentType `json:"document_type"`
	Content       string       `json:"content"`
}

func (DocumentReview) Type() Type { return TypeDocumentReview }

// ExportReport is an export of the event's Subject, an application: the
// directory it went to, the PDFs written there, and the documents skipped
// because they aren't drafted yet.
type ExportReport struct {
	Dir     string         `json:"dir"`
	Paths   []string       `json:"paths,omitempty"`
	Skipped []DocumentType `json:"skipped,omitempty"`
}

func (ExportReport) Type() Type { return TypeExportReport }

// ApplicationForm is what the event's Subject, a posting, asks for when
// you apply: each document it asks for, and its own questions. A document
// it doesn't ask for isn't in Documents.
type ApplicationForm struct {
	Documents map[DocumentType]FormRequirement `json:"documents,omitempty"`
	Questions []FormQuestion                   `json:"questions,omitempty"`
}

func (ApplicationForm) Type() Type { return TypeApplicationForm }

// FormQuestion is one question on an application form. Type is the
// board's field type, such as text or single_select, and Options a
// select's choices.
type FormQuestion struct {
	Label    string   `json:"label"`
	Required bool     `json:"required,omitzero"`
	Type     string   `json:"type,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// ChangeTick is that it's time to read the change log again.
type ChangeTick struct{}

func (ChangeTick) Type() Type { return TypeChangeTick }

// ChangePage is the change log's rows after a place in it, as the events
// they record, and the place after them.
type ChangePage struct {
	Events []tea.Msg `json:"events"`
	After  int64     `json:"after"`
}

func (ChangePage) Type() Type { return TypeChangePage }
