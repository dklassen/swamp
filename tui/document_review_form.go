package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

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
	documentType  store.DocumentType
	content       string
	textarea      textarea.Model
}

// newDocumentReviewFormModel returns a review-form screen for
// applicationID's documentType, sized to width/height.
func newDocumentReviewFormModel(s *store.Store, applicationID int64, documentType store.DocumentType, content string, width, height int) documentReviewFormModel {
	ta := textarea.New()
	ta.SetWidth(width)
	ta.SetHeight(height)
	ta.Focus()
	return documentReviewFormModel{
		store:         s,
		applicationID: applicationID,
		documentType:  documentType,
		content:       content,
		textarea:      ta,
	}
}

// cancelDocumentReviewFormMsg signals that App should switch back to the
// posting-detail screen without saving a review.
type cancelDocumentReviewFormMsg struct{}

type documentReviewCreatedMsg struct {
	review store.DocumentReview
	err    error
}

func createDocumentReview(s *store.Store, applicationID int64, documentType store.DocumentType, content string, outcome store.ReviewOutcome, notes string) tea.Cmd {
	return func() tea.Msg {
		review, err := s.CreateDocumentReview(context.Background(), applicationID, documentType, content, outcome, notes)
		return documentReviewCreatedMsg{review: review, err: err}
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
		return createDocumentReview(m.store, m.applicationID, m.documentType, m.content, store.ReviewOutcomePassed, m.textarea.Value()), nil
	case tea.KeyCtrlG:
		return createDocumentReview(m.store, m.applicationID, m.documentType, m.content, store.ReviewOutcomeFlagged, m.textarea.Value()), nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd, nil
}

// documentTypeLabel renders a DocumentType constant as human-readable
// text for screen titles.
func documentTypeLabel(documentType store.DocumentType) string {
	switch documentType {
	case store.DocumentTypeCoverLetter:
		return "cover letter"
	case store.DocumentTypeResume:
		return "resume"
	default:
		return documentType.String()
	}
}

func (m *documentReviewFormModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Review "+documentTypeLabel(m.documentType)) + "\n")
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(helpStyle.Render("ctrl+s: pass  ctrl+g: flag  esc: cancel"))
	return b.String()
}
