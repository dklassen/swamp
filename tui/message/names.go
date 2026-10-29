package message

import "fmt"

// Kind, Type, Op, NavOp, ErrorCode, Screen, ListingStatus,
// ApplicationStatus, DocumentType and ReviewOutcome are decided on, so Go
// code compares constants; each encodes as its name from one table, so
// reordering the constants can't change what a stored event means. A zero
// value is in its table only where "none" is legal.

type Kind int

const (
	_ Kind = iota
	KindChanged
	KindRead
	KindLoad
	KindRun
	KindRan
	KindError
	KindNav
	KindNotify
	KindSignal
)

var kindNames = [...]string{
	KindChanged: "changed",
	KindRead:    "read",
	KindLoad:    "load",
	KindRun:     "run",
	KindRan:     "ran",
	KindError:   "error",
	KindNav:     "nav",
	KindNotify:  "notify",
	KindSignal:  "signal",
}

func (k Kind) String() string                { return nameOf(k, kindNames[:]) }
func (k Kind) MarshalText() ([]byte, error)  { return marshalName(k, kindNames[:]) }
func (k *Kind) UnmarshalText(b []byte) error { return unmarshalName(k, b, kindNames[:]) }

// Type is what a payload is about within its kind.
type Type int

const (
	_ Type = iota
	TypeCompany
	TypeCompanyList
	TypeCompanyRow
	TypeSyncCompany
	TypeSyncAll
	TypeSyncReport
	TypeSyncAllReport
	TypeContentSize
	TypeFocusChanged
	TypeStatus
	TypeError
	TypeNone
	TypePostingList
	TypeHideArchived
	TypeFilterSelection
	TypeFilterOptions
	TypePosting
	TypeMarkup
	TypeApplication
	TypePostingDetail
	TypePostingApplication
	TypeReview
	TypeReviewList
	TypePostingScores
)

var typeNames = [...]string{
	TypeCompany:            "company",
	TypeCompanyList:        "company_list",
	TypeCompanyRow:         "company_row",
	TypeSyncCompany:        "sync_company",
	TypeSyncAll:            "sync_all",
	TypeSyncReport:         "sync_report",
	TypeSyncAllReport:      "sync_all_report",
	TypeContentSize:        "content_size",
	TypeFocusChanged:       "focus_changed",
	TypeStatus:             "status",
	TypeError:              "error",
	TypeNone:               "none",
	TypePostingList:        "posting_list",
	TypeHideArchived:       "hide_archived",
	TypeFilterSelection:    "filter_selection",
	TypeFilterOptions:      "filter_options",
	TypePosting:            "posting",
	TypeMarkup:             "markup",
	TypeApplication:        "application",
	TypePostingDetail:      "posting_detail",
	TypePostingApplication: "posting_application",
	TypeReview:             "review",
	TypeReviewList:         "review_list",
	TypePostingScores:      "posting_scores",
}

func (t Type) String() string                { return nameOf(t, typeNames[:]) }
func (t Type) MarshalText() ([]byte, error)  { return marshalName(t, typeNames[:]) }
func (t *Type) UnmarshalText(b []byte) error { return unmarshalName(t, b, typeNames[:]) }

// Op is what a changed event wrote, or what an error event didn't. A
// request has OpNone.
type Op int

const (
	OpNone Op = iota
	OpCreated
	OpUpdated
	OpDeleted
)

var opNames = [...]string{
	OpNone:    "none",
	OpCreated: "created",
	OpUpdated: "updated",
	OpDeleted: "deleted",
}

func (o Op) String() string                { return nameOf(o, opNames[:]) }
func (o Op) MarshalText() ([]byte, error)  { return marshalName(o, opNames[:]) }
func (o *Op) UnmarshalText(b []byte) error { return unmarshalName(o, b, opNames[:]) }

type NavOp int

const (
	_ NavOp = iota
	NavPush
	NavPop
	NavReplace
)

var navOpNames = [...]string{
	NavPush:    "push",
	NavPop:     "pop",
	NavReplace: "replace",
}

func (o NavOp) String() string                { return nameOf(o, navOpNames[:]) }
func (o NavOp) MarshalText() ([]byte, error)  { return marshalName(o, navOpNames[:]) }
func (o *NavOp) UnmarshalText(b []byte) error { return unmarshalName(o, b, navOpNames[:]) }

type ErrorCode int

const (
	_ ErrorCode = iota
	ErrNotFound
	ErrSyncInProgress
	ErrOther
	ErrFault
	ErrPostingClosed
)

var errorCodeNames = [...]string{
	ErrNotFound:       "not_found",
	ErrSyncInProgress: "sync_in_progress",
	ErrOther:          "other",
	ErrFault:          "fault",
	ErrPostingClosed:  "posting_closed",
}

func (c ErrorCode) String() string                { return nameOf(c, errorCodeNames[:]) }
func (c ErrorCode) MarshalText() ([]byte, error)  { return marshalName(c, errorCodeNames[:]) }
func (c *ErrorCode) UnmarshalText(b []byte) error { return unmarshalName(c, b, errorCodeNames[:]) }

// Screen is a kind of screen a Nav opens. A pop opens none.
type Screen int

const (
	ScreenNone Screen = iota
	ScreenCompanyForm
	ScreenCompanyEdit
	ScreenCompanyDelete
	ScreenCompanyPostings
	ScreenFilterSelect
	ScreenPostingDetail
)

var screenNames = [...]string{
	ScreenNone:            "none",
	ScreenCompanyForm:     "company_form",
	ScreenCompanyEdit:     "company_edit",
	ScreenCompanyDelete:   "company_delete",
	ScreenCompanyPostings: "company_postings",
	ScreenFilterSelect:    "filter_select",
	ScreenPostingDetail:   "posting_detail",
}

func (s Screen) String() string                { return nameOf(s, screenNames[:]) }
func (s Screen) MarshalText() ([]byte, error)  { return marshalName(s, screenNames[:]) }
func (s *Screen) UnmarshalText(b []byte) error { return unmarshalName(s, b, screenNames[:]) }

// ListingStatus is whether a posting's job board still lists it.
type ListingStatus int

const (
	_ ListingStatus = iota
	ListingOpen
	ListingClosed
)

var listingStatusNames = [...]string{
	ListingOpen:   "open",
	ListingClosed: "closed",
}

func (s ListingStatus) String() string               { return nameOf(s, listingStatusNames[:]) }
func (s ListingStatus) MarshalText() ([]byte, error) { return marshalName(s, listingStatusNames[:]) }
func (s *ListingStatus) UnmarshalText(b []byte) error {
	return unmarshalName(s, b, listingStatusNames[:])
}

// ApplicationStatus is where the user is with an application.
type ApplicationStatus int

const (
	_ ApplicationStatus = iota
	ApplicationStarted
	ApplicationSubmitted
	ApplicationInterviewing
	ApplicationRejected
	ApplicationOfferReceived
	ApplicationOfferAccepted
	ApplicationOfferDeclined
	ApplicationPostingClosed
	ApplicationWithdrawn
)

var applicationStatusNames = [...]string{
	ApplicationStarted:       "application_started",
	ApplicationSubmitted:     "application_submitted",
	ApplicationInterviewing:  "interviewing",
	ApplicationRejected:      "rejected",
	ApplicationOfferReceived: "offer_received",
	ApplicationOfferAccepted: "offer_accepted",
	ApplicationOfferDeclined: "offer_declined",
	ApplicationPostingClosed: "posting_closed",
	ApplicationWithdrawn:     "withdrawn",
}

func (s ApplicationStatus) String() string { return nameOf(s, applicationStatusNames[:]) }
func (s ApplicationStatus) MarshalText() ([]byte, error) {
	return marshalName(s, applicationStatusNames[:])
}
func (s *ApplicationStatus) UnmarshalText(b []byte) error {
	return unmarshalName(s, b, applicationStatusNames[:])
}

// DocumentType is which of an application's documents something is about.
type DocumentType int

const (
	_ DocumentType = iota
	DocumentCoverLetter
	DocumentResume
)

var documentTypeNames = [...]string{
	DocumentCoverLetter: "cover_letter",
	DocumentResume:      "resume",
}

func (d DocumentType) String() string               { return nameOf(d, documentTypeNames[:]) }
func (d DocumentType) MarshalText() ([]byte, error) { return marshalName(d, documentTypeNames[:]) }
func (d *DocumentType) UnmarshalText(b []byte) error {
	return unmarshalName(d, b, documentTypeNames[:])
}

// ReviewOutcome is what a review of a document found.
type ReviewOutcome int

const (
	_ ReviewOutcome = iota
	ReviewPassed
	ReviewFlagged
)

var reviewOutcomeNames = [...]string{
	ReviewPassed:  "passed",
	ReviewFlagged: "flagged",
}

func (o ReviewOutcome) String() string               { return nameOf(o, reviewOutcomeNames[:]) }
func (o ReviewOutcome) MarshalText() ([]byte, error) { return marshalName(o, reviewOutcomeNames[:]) }
func (o *ReviewOutcome) UnmarshalText(b []byte) error {
	return unmarshalName(o, b, reviewOutcomeNames[:])
}

// An empty entry is a value with no name, such as the zero Kind.
func nameOf[E ~int](e E, names []string) string {
	if e < 0 || int(e) >= len(names) || names[e] == "" {
		return fmt.Sprintf("%T(%d)", e, int(e))
	}
	return names[e]
}

func marshalName[E ~int](e E, names []string) ([]byte, error) {
	if e < 0 || int(e) >= len(names) || names[e] == "" {
		return nil, fmt.Errorf("message: %T(%d) has no name", e, int(e))
	}
	return []byte(names[e]), nil
}

func unmarshalName[E ~int](e *E, b []byte, names []string) error {
	for i, name := range names {
		if name != "" && name == string(b) {
			*e = E(i)
			return nil
		}
	}
	return fmt.Errorf("message: unknown %T %q", *e, b)
}
