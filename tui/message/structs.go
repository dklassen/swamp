package message

import (
	tea "charm.land/bubbletea/v2"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/sync"
)

type (
	Screen         int
	ScreenInstance int
)

type CompaniesLoadedMsg struct {
	Companies []company.Company
	// OpenPostings is each company's open, unarchived posting count.
	OpenPostings map[int64]int
	Err          error
}

// ContentSizeMsg is the area a screen draws in: the terminal less the
// header and banner, which grow and shrink without the terminal resizing.
type ContentSizeMsg struct {
	Width int
	Rows  int
}

// FocusChangedMsg names the component now in focus, nil when none is.
type FocusChangedMsg struct{ Focused tea.Model }

// BannerMsg is how a screen reports to the banner.
type BannerMsg struct {
	Err    error
	Status string
	// Load: a load's result. Loads finish in any order, so a successful
	// one leaves another load's error in place.
	Load bool
}

// ErrMsg is a fault in the program, such as a handler given a message it
// wasn't registered for, which nothing the user does can fix. It isn't
// meant to be shown; the router logs it.
type ErrMsg struct{ Err error }

// Fail sends err as an ErrMsg.
func Fail(err error) tea.Cmd { return func() tea.Msg { return ErrMsg{Err: err} } }

func ReportErr(err error) tea.Cmd   { return func() tea.Msg { return BannerMsg{Err: err} } }
func ReportLoad(err error) tea.Cmd  { return func() tea.Msg { return BannerMsg{Err: err, Load: true} } }
func ReportStatus(s string) tea.Cmd { return func() tea.Msg { return BannerMsg{Status: s} } }

// RefreshCompanyMsg asks for Company to be refreshed, by whatever knows
// whether another sync (e.g. a sync-all run) is already under way.
type RefreshCompanyMsg struct{ Company company.Company }

// CompanyRefreshedMsg is a refresh's result. Name is the company it was
// started for, since a refused refresh has no Result.
type CompanyRefreshedMsg struct {
	Name   string
	Result sync.Result
	Err    error
}

// ToggleSyncAllMsg starts a sync-all run over Companies, or stops the one
// under way.
type ToggleSyncAllMsg struct{ Companies []company.Company }

// SyncAllStepMsg is one company's result within run RunID.
type SyncAllStepMsg struct {
	RunID  int
	Result sync.Result
}

// SyncAllFinishedMsg is a run's results once it has ended, finished or
// stopped.
type SyncAllFinishedMsg struct{ Results []sync.Result }

// OpenCompanyFormMsg asks for the form that adds a company.
type OpenCompanyFormMsg struct{}

// OpenCompanyEditMsg asks for Company's edit form.
type OpenCompanyEditMsg struct{ Company company.Company }

// OpenCompanyDeleteMsg asks for the confirmation before deleting Company,
// which has OpenPostings open postings.
type OpenCompanyDeleteMsg struct {
	Company      company.Company
	OpenPostings int
}

// OpenCompanyPostingsMsg asks for Company's postings.
type OpenCompanyPostingsMsg struct{ Company company.Company }

type CompanyCreatedMsg struct {
	Company company.Company
	Err     error
}

type CompanyDeletedMsg struct {
	CompanyID int64
	Err       error
}

type CompanyNameUpdatedMsg struct {
	Company company.Company
	Err     error
}
