package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// documentReviewFormModel drives the document-review-form screen: enter
// notes and submit a review outcome for one document. It holds the store
// it needs to save the review, the application/document it's reviewing
// (fixed for this screen's lifetime), the content read from disk when
// the review-select screen was entered (so the review's snapshot is
// exactly what was shown to the user, not whatever the file happens to
// contain by the time this form is submitted), and its own private
// textarea.
type documentReviewFormModel struct {
	store         *store.Store
	applicationID int64
	documentType  documents.Type
	content       string
	textarea      textarea.Model
	// instance tells this screen apart from later openings of it; the
	// save's result records it as from (#115).
	instance screenInstance
}

// newDocumentReviewFormModel returns a review-form screen for
// applicationID's documentType, sized to width and height, the rows App
// leaves under its status/error banner (App.screenRows). App refits the
// height with setHeight whenever that changes; the width is fixed at
// construction. instance tells it apart from later openings.
func newDocumentReviewFormModel(s *store.Store, applicationID int64, documentType documents.Type, content string, width, height int, instance screenInstance) documentReviewFormModel {
	ta := textarea.New()
	ta.SetWidth(width)
	ta.Focus()
	m := documentReviewFormModel{
		store:         s,
		applicationID: applicationID,
		documentType:  documentType,
		content:       content,
		textarea:      ta,
		instance:      instance,
	}
	m.setHeight(height)
	return m
}

// setHeight fits the screen into height terminal rows, giving the text
// area whatever the title and help line, measured as rendered, leave
// (issue #138).
func (m *documentReviewFormModel) setHeight(height int) {
	m.textarea.SetHeight(max(height-lipgloss.Height(m.title())-lipgloss.Height(documentReviewFormHelp()), 0))
}

func (m *documentReviewFormModel) title() string {
	return titleStyle.Render("Review " + m.documentType.Label())
}

func documentReviewFormHelp() string {
	return helpStyle.Render("ctrl+s: pass  ctrl+g: flag  esc: cancel")
}

// cancelDocumentReviewFormMsg signals that App should switch back to the
// posting-detail screen without saving a review.
type cancelDocumentReviewFormMsg struct{}

type documentReviewCreatedMsg struct {
	review store.DocumentReview
	// from is the review form instance that started the save (#115).
	from screenInstance
	err  error
}

func createDocumentReview(s *store.Store, applicationID int64, documentType documents.Type, content string, outcome store.ReviewOutcome, notes string, from screenInstance) tea.Cmd {
	return func() tea.Msg {
		review, err := s.CreateDocumentReview(context.Background(), applicationID, documentType, content, outcome, notes)
		return documentReviewCreatedMsg{review: review, from: from, err: err}
	}
}

// Submit bindings are ctrl+s (passed) and ctrl+g (flagged) -- deliberately
// not ctrl+p/ctrl+f, which bubbles/textarea's own DefaultKeyMap already
// binds to "previous line" and "character forward" respectively; using
// them here would silently submit the review whenever the user tries to
// move the cursor while editing multi-line notes.
func (m *documentReviewFormModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch msg.Type {
	case tea.KeyEsc:
		return nil, cancelDocumentReviewFormMsg{}
	case tea.KeyCtrlS:
		return createDocumentReview(m.store, m.applicationID, m.documentType, m.content, store.ReviewOutcomePassed, m.textarea.Value(), m.instance), nil
	case tea.KeyCtrlG:
		return createDocumentReview(m.store, m.applicationID, m.documentType, m.content, store.ReviewOutcomeFlagged, m.textarea.Value(), m.instance), nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd, nil
}

func (m *documentReviewFormModel) View() string {
	var b strings.Builder
	b.WriteString(m.title() + "\n")
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(documentReviewFormHelp())
	return b.String()
}
