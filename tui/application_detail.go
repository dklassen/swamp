package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// applicationDetailModel drives the application-detail screen: the place
// application-specific actions live -- navigating to the related
// posting, editing the cover letter/resume in $EDITOR, and reviewing
// each of them (see the #86 follow-up reorganizing the
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
	case msg.String() == "u":
		return nil, refreshApplicationDetailMsg{}
	case msg.String() == "S":
		return nil, enterApplicationSubmitMsg{application: m.application}
	case msg.String() == "f":
		return nil, enterApplicationFormMsg{postingID: m.application.Posting.ID}
	case msg.String() == "D":
		return nil, enterApplicationDeleteMsg{application: m.application}
	}
	// Each document type's key edits it, and the uppercase key reviews it.
	for _, documentType := range documents.Types() {
		switch msg.String() {
		case string(documentType.Key()):
			return m.openDocument(documentType), nil
		case string(unicode.ToUpper(documentType.Key())):
			return nil, m.enterReview(documentType)
		}
	}
	return nil, nil
}

// refreshApplicationDetailMsg signals that App should reload this
// application's document reviews from the store without leaving
// application detail -- e.g. after an external agent (see
// .agents/skills/apply-to-posting) revises a document on disk while the
// user is still looking at this screen. Document
// existence itself needs no reload: View reads m.documents.Status live
// on every render.
type refreshApplicationDetailMsg struct{}

// openDocument ensures the application's document directory exists (most
// editors create the file itself on save, but not the directory) and
// returns a command that opens documentType's document in $EDITOR -- moved here from activeApplicationListModel
// because editing a specific application's documents is
// application-specific functionality, not something the cross-company
// list screen should own directly.
func (m *applicationDetailModel) openDocument(documentType documents.Type) tea.Cmd {
	if _, err := m.documents.EnsureDir(m.application.ID); err != nil {
		return func() tea.Msg { return editorClosedMsg{err: err} }
	}
	path, err := m.documents.Path(m.application.ID, documentType)
	if err != nil {
		return func() tea.Msg { return editorClosedMsg{err: err} }
	}
	before, err := m.documents.SHA256(m.application.ID, documentType)
	if err != nil {
		return func() tea.Msg { return editorClosedMsg{err: err} }
	}
	return openInEditor(path, editorClosedMsg{applicationID: m.application.ID, documentType: documentType, before: before})
}

// enterReview reads documentType's current content off disk (if it
// exists) and returns the message that starts the review-form screen
// for it directly -- unlike documentReviewSelectModel's two-step picker
// (still used from posting detail's 'r'), this screen already knows
// which document via which key was pressed (L for cover letter, R for
// resume), so there's no picker step. Returns nil (no-op) if the
// document doesn't exist yet -- nothing to review.
func (m *applicationDetailModel) enterReview(documentType documents.Type) tea.Msg {
	status := m.documents.Status(m.application.ID)
	doc, err := status.Doc(documentType)
	if err != nil {
		return enterDocumentReviewFormMsg{err: err}
	}
	if !doc.Exists {
		return nil
	}
	return readDocumentForReview(m.application.ID, documentType, doc.Path)
}

func (m *applicationDetailModel) View() string {
	var b strings.Builder
	b.WriteString(fieldLabel.Render("Company:") + " " + m.application.CompanyName + "\n")
	b.WriteString(fieldLabel.Render("Status:") + " " + applicationStatusLabel(m.application.Status) + "\n")
	if m.application.Notes != "" {
		b.WriteString(fieldLabel.Render("Notes:") + " " + m.application.Notes + "\n")
	}

	status := m.documents.Status(m.application.ID)
	b.WriteString("\n" + fieldLabel.Render("Documents") + "\n")
	for _, documentType := range documents.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			continue
		}
		review, hasReview := m.application.LatestReviews[documentType]
		b.WriteString(documentStatusLine(documentTitle(documentType), doc.Exists, doc.Path, review, hasReview))
	}

	help := []string{"p: view posting"}
	for _, documentType := range documents.Types() {
		help = append(help, string(documentType.Key())+": edit "+documentType.Label())
	}
	for _, documentType := range documents.Types() {
		help = append(help, string(unicode.ToUpper(documentType.Key()))+": review "+documentType.Label())
	}
	help = append(help, "f: application form", "S: submit", "D: delete", "u: refresh", "esc/b: back")
	b.WriteString(helpStyle.Render(strings.Join(help, "  ")))
	return b.String()
}

// documentTitle is documentType's label in title case, for row headings
// ("Cover Letter").
func documentTitle(documentType documents.Type) string {
	words := strings.Fields(documentType.Label())
	for i, word := range words {
		r := []rune(word)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
