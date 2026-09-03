package tui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// documentReviewOption is one selectable document on the document-review-
// select screen: its type, resolved path, and whether it exists yet (an
// agent may not have drafted it).
type documentReviewOption struct {
	label        string
	documentType string
	path         string
	exists       bool
}

// documentReviewSelectModel drives the document-review-select screen for
// a single application: pick which document (cover letter or resume) to
// review. It holds the documents store it needs to resolve paths -- and
// reads content directly off disk itself when a selection is made, since
// documents deliberately never reads file content (see that package's
// doc comment); Swamp only starts doing so here, specifically to capture
// a review snapshot (see decisions.log, #51) -- the posting/application
// it's reviewing for, and its own private cursor.
type documentReviewSelectModel struct {
	documents     *documents.Store
	postingID     int64
	applicationID int64
	options       []documentReviewOption
	cursor        int
}

// newDocumentReviewSelectModel returns a review-select screen for
// applicationID, with options seeded from its current document status.
func newDocumentReviewSelectModel(docs *documents.Store, postingID, applicationID int64) documentReviewSelectModel {
	status := docs.Status(applicationID)
	return documentReviewSelectModel{
		documents:     docs,
		postingID:     postingID,
		applicationID: applicationID,
		options: []documentReviewOption{
			{label: "Cover Letter", documentType: store.DocumentTypeCoverLetter, path: status.CoverLetter.Path, exists: status.CoverLetter.Exists},
			{label: "Resume", documentType: store.DocumentTypeResume, path: status.Resume.Path, exists: status.Resume.Exists},
		},
	}
}

// cancelDocumentReviewSelectMsg signals that App should switch back to
// the posting-detail screen without starting a review.
type cancelDocumentReviewSelectMsg struct{}

// enterDocumentReviewFormMsg signals that App should switch to the
// document-review-form screen, seeded with content read from disk at
// selection time -- the review's eventual snapshot is exactly what was
// read here, not whatever the file contains by the time the form is
// submitted. err is set if the read failed (e.g. the file was deleted
// between the existence check and now); App surfaces it as a.err and
// stays on the select screen rather than entering the form with no
// content.
type enterDocumentReviewFormMsg struct {
	postingID     int64
	applicationID int64
	documentType  string
	content       string
	err           error
}

func (m *documentReviewSelectModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.Type == tea.KeyEsc, msg.String() == "b":
		return nil, cancelDocumentReviewSelectMsg{}
	case msg.Type == tea.KeyEnter:
		opt := m.options[m.cursor]
		if !opt.exists {
			return nil, nil
		}
		content, err := os.ReadFile(opt.path)
		return nil, enterDocumentReviewFormMsg{
			postingID:     m.postingID,
			applicationID: m.applicationID,
			documentType:  opt.documentType,
			content:       string(content),
			err:           err,
		}
	}
	return nil, nil
}

func (m *documentReviewSelectModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Review a document") + "\n")
	for i, opt := range m.options {
		status := "not found"
		if opt.exists {
			status = "found"
		}
		line := opt.label + " (" + status + ")"
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: review  esc/b: cancel"))
	return b.String()
}
