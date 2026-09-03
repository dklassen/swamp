package tui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// applicationDetailModel drives the application-detail screen: the place
// application-specific actions live -- navigating to the related
// posting, editing the cover letter/resume in $EDITOR, and reviewing
// each of them (see decisions.log, the #86 follow-up reorganizing the
// active-applications ("application view") workflow). It holds only the
// dependencies it needs for its own commands and the ApplicationView it
// was constructed with -- a snapshot, not a live reference into App's
// activeApplications slice, refreshed by App whenever the underlying
// data changes (mirroring postingDetailModel's own convention).
type applicationDetailModel struct {
	documents   *documents.Store
	application store.ApplicationView
}

func newApplicationDetailModel(docs *documents.Store, application store.ApplicationView) applicationDetailModel {
	return applicationDetailModel{documents: docs, application: application}
}

func (m *applicationDetailModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyEsc, msg.String() == "b":
		return nil, backToActiveApplicationsMsg{}
	case msg.String() == "p":
		return nil, enterPostingDetailMsg{postingID: m.application.Posting.ID}
	case msg.String() == "l":
		return m.openDocument(false), nil
	case msg.String() == "r":
		return m.openDocument(true), nil
	case msg.String() == "L":
		return nil, m.enterReview(store.DocumentTypeCoverLetter)
	case msg.String() == "R":
		return nil, m.enterReview(store.DocumentTypeResume)
	}
	return nil, nil
}

// openDocument ensures the application's document directory exists (most
// editors create the file itself on save, but not the directory) and
// returns a command that opens the cover letter (resume=false) or resume
// (resume=true) in $EDITOR -- moved here from activeApplicationListModel
// (see decisions.log): editing a specific application's documents is
// application-specific functionality, not something the cross-company
// list screen should own directly.
func (m *applicationDetailModel) openDocument(resume bool) tea.Cmd {
	paths, err := m.documents.EnsureDir(m.application.ID)
	if err != nil {
		return func() tea.Msg { return editorClosedMsg{err: err} }
	}
	path := paths.CoverLetter
	if resume {
		path = paths.Resume
	}
	return openInEditor(path)
}

// enterReview reads documentType's current content off disk (if it
// exists) and returns the message that starts the review-form screen
// for it directly -- unlike documentReviewSelectModel's two-step picker
// (still used from posting detail's 'r'), this screen already knows
// which document via which key was pressed (L for cover letter, R for
// resume), so there's no picker step. Returns nil (no-op) if the
// document doesn't exist yet -- nothing to review.
func (m *applicationDetailModel) enterReview(documentType store.DocumentType) tea.Msg {
	status := m.documents.Status(m.application.ID)
	doc := status.CoverLetter
	if documentType == store.DocumentTypeResume {
		doc = status.Resume
	}
	if !doc.Exists {
		return nil
	}
	content, err := os.ReadFile(doc.Path)
	return enterDocumentReviewFormMsg{
		applicationID: m.application.ID,
		documentType:  documentType,
		content:       string(content),
		err:           err,
	}
}

func (m *applicationDetailModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.application.Posting.Title) + "\n")
	b.WriteString(fieldLabel.Render("Company:") + " " + m.application.CompanyName + "\n")
	b.WriteString(fieldLabel.Render("Status:") + " " + m.application.Status.String() + "\n")
	if m.application.Notes != "" {
		b.WriteString(fieldLabel.Render("Notes:") + " " + m.application.Notes + "\n")
	}

	status := m.documents.Status(m.application.ID)
	b.WriteString("\n" + fieldLabel.Render("Documents") + "\n")
	clReview, hasCLReview := m.application.LatestReviews[store.DocumentTypeCoverLetter]
	b.WriteString(documentStatusLine("Cover Letter", status.CoverLetter.Exists, status.CoverLetter.Path, clReview, hasCLReview))
	resumeReview, hasResumeReview := m.application.LatestReviews[store.DocumentTypeResume]
	b.WriteString(documentStatusLine("Resume", status.Resume.Exists, status.Resume.Path, resumeReview, hasResumeReview))

	b.WriteString(helpStyle.Render("p: view posting  l: edit cover letter  r: edit resume  L: review cover letter  R: review resume  esc/b: back"))
	return b.String()
}
