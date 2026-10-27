package message

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/sync"
)

type ScreenInstance int

// Fail reports err as a fault in the program, such as a handler given a
// message it wasn't registered for, which nothing the user does can fix.
func Fail(src Source, err error) tea.Cmd {
	return func() tea.Msg {
		return New(src, Subject{}, OpNone, time.Now().UTC(), Error{Code: ErrFault, Text: err.Error()})
	}
}

// ReportStatus puts s in the banner, or clears it if s is "".
func ReportStatus(src Source, s string) tea.Cmd {
	return func() tea.Msg {
		return New(src, Subject{}, OpNone, time.Now().UTC(), Notify{Status: s})
	}
}

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

// CloseCompanyFormMsg asks for the add-company form to close.
type CloseCompanyFormMsg struct{}

// OpenCompanyEditMsg asks for Company's edit form.
type OpenCompanyEditMsg struct{ Company company.Company }

// CloseCompanyEditMsg asks for the company edit to close.
type CloseCompanyEditMsg struct{}

// OpenCompanyDeleteMsg asks for the confirmation before deleting Company,
// which has OpenPostings open postings.
type OpenCompanyDeleteMsg struct {
	Company      company.Company
	OpenPostings int
}

// CloseCompanyDeleteMsg asks for the delete confirmation to close.
type CloseCompanyDeleteMsg struct{}

// OpenCompanyPostingsMsg asks for Company's postings.
type OpenCompanyPostingsMsg struct{ Company company.Company }
