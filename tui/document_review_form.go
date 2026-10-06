package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/aymanbagabas/go-udiff"
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
	documents     *documents.Store
	applicationID int64
	documentType  documents.Type
	content       string
	textarea      textarea.Model
	instance      screenInstance
	// changes is set when a save found the document rewritten since the
	// form read it: the diff from that version to content, which is now
	// the current one.
	changes string
	who     string
	height  int
}

// newDocumentReviewFormModel returns a review-form screen for
// applicationID's documentType, sized to width and height, the rows App
// leaves under its status/error banner (App.screenRows). App refits the
// height with setHeight whenever that changes; the width is fixed at
// construction.
func newDocumentReviewFormModel(s *store.Store, docs *documents.Store, applicationID int64, documentType documents.Type, content string, width, height int, instance screenInstance) documentReviewFormModel {
	ta := textarea.New()
	ta.SetWidth(width)
	ta.Focus()
	m := documentReviewFormModel{
		store:         s,
		documents:     docs,
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
	m.height = height
	used := lipgloss.Height(m.title()) + lipgloss.Height(documentReviewFormHelp())
	if m.changes != "" {
		used += lipgloss.Height(m.changesView())
	}
	m.textarea.SetHeight(max(height-used, 0))
}

// reload switches the form to current, the document as it is now, after a
// save found it changed. The notes typed so far are kept, and the
// changes are shown so you can check they still apply.
func (m *documentReviewFormModel) reload(current, who string) {
	m.changes = udiff.Unified("what you read", "on disk now", m.content, current)
	m.who = who
	m.content = current
	m.setHeight(m.height)
}

// changesView is the notice and diff reload shows, cut to half the
// screen so the notes stay usable.
func (m *documentReviewFormModel) changesView() string {
	lines := strings.Split(strings.TrimRight(m.changes, "\n"), "\n")
	if limit := max(m.height/2, 3); len(lines) > limit {
		lines = append(lines[:limit], fmt.Sprintf("... %d more lines", len(lines)-limit))
	}
	notice := "The document changed while you were reviewing, so nothing was saved."
	if m.who != "" {
		notice = "The document was " + m.who + " while you were reviewing, so nothing was saved."
	}
	notice += " This is what changed; it's what you're reviewing now. Check your notes still apply, then save again."
	return warnStyle.Render(notice) + "\n" + strings.Join(lines, "\n") + "\n"
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
	from   screenInstance
	err    error
}

// documentChangedDuringReviewMsg reports that the document on disk is no
// longer what the form showed, so nothing was saved.
type documentChangedDuringReviewMsg struct {
	current string
	// who is empty when Swamp didn't record the write (an edit made
	// outside Swamp).
	who  string
	from screenInstance
}

// createDocumentReview saves a review of content, unless the file no
// longer holds it: an agent or $EDITOR may have rewritten it while the
// form was open. A review of a version that's gone would
// never count as current, and an agent would never see its notes, so
// instead it reports the current content for the form to show.
func createDocumentReview(s *store.Store, docs *documents.Store, applicationID int64, documentType documents.Type, content string, outcome store.ReviewOutcome, notes string, from screenInstance) tea.Cmd {
	return func() tea.Msg {
		path, err := docs.Path(applicationID, documentType)
		if err != nil {
			return documentReviewCreatedMsg{from: from, err: err}
		}
		current, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return documentReviewCreatedMsg{from: from, err: errors.New("the document was deleted while you were reviewing it, so the review wasn't saved")}
		}
		if err != nil {
			return documentReviewCreatedMsg{from: from, err: fmt.Errorf("check the document before saving the review: %w", err)}
		}
		if string(current) != content {
			return documentChangedDuringReviewMsg{current: string(current), who: whoWrote(s, applicationID, documentType, string(current)), from: from}
		}
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
		return createDocumentReview(m.store, m.documents, m.applicationID, m.documentType, m.content, store.ReviewOutcomePassed, m.textarea.Value(), m.instance), nil
	case tea.KeyCtrlG:
		return createDocumentReview(m.store, m.documents, m.applicationID, m.documentType, m.content, store.ReviewOutcomeFlagged, m.textarea.Value(), m.instance), nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd, nil
}

func (m *documentReviewFormModel) View() string {
	var b strings.Builder
	b.WriteString(m.title() + "\n")
	if m.changes != "" {
		b.WriteString(m.changesView())
	}
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(documentReviewFormHelp())
	return b.String()
}

// whoWrote describes the recorded write that produced current, e.g.
// "rewritten by the agent at 14:05", or "" if the latest recorded write
// is of some other version (the change was made outside Swamp).
func whoWrote(s *store.Store, applicationID int64, documentType documents.Type, current string) string {
	write, ok, err := s.LatestDocumentWrite(context.Background(), applicationID, documentType)
	if err != nil {
		return fmt.Sprintf("rewritten (couldn't tell by whom: %v)", err)
	}
	if !ok || write.ContentSHA256 != documents.ContentSHA256(current) {
		return ""
	}
	return "rewritten by " + documentWriterLabel(write.Source) + " at " + localClock(write.WrittenAt)
}

// documentWriterLabel is who a write source is, for a sentence.
func documentWriterLabel(source store.DocumentWriteSource) string {
	switch source {
	case store.DocumentWriteSourceWriteDocument:
		return "the agent"
	case store.DocumentWriteSourceEditor:
		return "you, in $EDITOR,"
	default:
		return source.String()
	}
}

// localClock shows t in local time: the time alone if it's today, with
// the date otherwise. Stored times are UTC (AGENTS.md); converting is for
// display only.
func localClock(t time.Time) string {
	local, now := t.Local(), time.Now()
	if local.YearDay() == now.YearDay() && local.Year() == now.Year() {
		return local.Format("15:04")
	}
	return local.Format("Jan 2 15:04")
}
