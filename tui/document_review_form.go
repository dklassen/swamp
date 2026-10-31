package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aymanbagabas/go-udiff"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// documentReviewFormModel takes notes on one document and saves them as a
// review, passed or flagged, of the content it opened on: what was shown,
// not whatever the file holds by the time it's saved.
type documentReviewFormModel struct {
	documentService *service.DocumentService
	documents       *documents.Store
	review          message.DocumentReview
	textarea        textarea.Model
	// changes is set when a save found the document rewritten since the
	// form read it: the diff from that version to review.Content, which is
	// now the current one.
	changes  string
	height   int
	focused  bool
	instance message.ScreenInstance
}

// newDocumentReviewFormModel opens on d, sized to size. The width is fixed
// at construction.
func newDocumentReviewFormModel(documentService *service.DocumentService, docs *documents.Store, d message.DocumentReview, size message.ContentSize) *documentReviewFormModel {
	ta := textarea.New()
	ta.SetWidth(size.Width)
	ta.Focus()
	m := &documentReviewFormModel{
		documentService: documentService,
		documents:       docs,
		review:          d,
		textarea:        ta,
		instance:        message.NewScreenInstance(),
	}
	m.setHeight(size.Rows)
	return m
}

func (m *documentReviewFormModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *documentReviewFormModel) Instance() message.ScreenInstance { return m.instance }

func (m *documentReviewFormModel) Focused() bool { return m.focused }

func (m *documentReviewFormModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.saved, model.From(m.instance)),
		model.Always(m.saveFailed, model.From(m.instance)),
		model.WhenFocused(m, m.keyPressed),
		model.WhenFocused(m, m.paste),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *documentReviewFormModel) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the form handles is registered.
func (m *documentReviewFormModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *documentReviewFormModel) saved(message.Event[message.Changed[message.Review]]) tea.Cmd {
	return m.close()
}

// saveFailed keeps the notes for another go; the banner shows why. A
// document that changed is read again, so the next save reviews it as it
// is.
func (m *documentReviewFormModel) saveFailed(e message.Event[message.Error]) tea.Cmd {
	if e.Payload.Code != message.ErrDocumentChanged {
		return nil
	}
	path, err := m.documents.Path(m.review.ApplicationID, documentTypeOf(m.review.DocumentType))
	if err != nil {
		return reviewFailed(m.src(), m.review.ApplicationID, err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return reviewFailed(m.src(), m.review.ApplicationID, fmt.Errorf("read the changed document: %w", err))
	}
	m.reload(string(current))
	return nil
}

func (m *documentReviewFormModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.setHeight(e.Payload.Value.Rows)
	return nil
}

func (m *documentReviewFormModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

// close takes a failed save's error from the banner with the form.
func (m *documentReviewFormModel) close() tea.Cmd {
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), ""))
}

// setHeight fits the screen into height terminal rows, giving the text
// area whatever the help line, measured as rendered, leaves
// (issue #138).
func (m *documentReviewFormModel) setHeight(height int) {
	m.height = height
	used := lipgloss.Height(documentReviewFormHelp())
	if m.changes != "" {
		used += lipgloss.Height(m.changesView())
	}
	m.textarea.SetHeight(max(height-used, 0))
}

// reload switches the form to current, the document as it is now, after a
// save found it changed. The notes typed so far are kept, and the
// changes are shown so you can check they still apply.
func (m *documentReviewFormModel) reload(current string) {
	m.changes = udiff.Unified("what you read", "on disk now", m.review.Content, current)
	m.review.Content = current
	m.setHeight(m.height)
}

// changesView is the diff reload shows, cut to half the screen so the
// notes stay usable. The banner says who changed the document.
func (m *documentReviewFormModel) changesView() string {
	lines := strings.Split(strings.TrimRight(m.changes, "\n"), "\n")
	if limit := max(m.height/2, 3); len(lines) > limit {
		lines = append(lines[:limit], fmt.Sprintf("... %d more lines", len(lines)-limit))
	}
	notice := "This is what changed; it's what you're reviewing now. Check your notes still apply, then save again."
	return model.WarnStyle.Render(notice) + "\n" + strings.Join(lines, "\n") + "\n"
}

func documentReviewFormHelp() string {
	return model.HelpStyle.Render("ctrl+s: pass  ctrl+g: flag  esc: cancel")
}

// Submit bindings are ctrl+s (passed) and ctrl+g (flagged) -- deliberately
// not ctrl+p/ctrl+f, which bubbles/textarea's own DefaultKeyMap already
// binds to "previous line" and "character forward" respectively; using
// them here would silently submit the review whenever the user tries to
// move the cursor while editing multi-line notes.
func (m *documentReviewFormModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return m.close()
	case "ctrl+s":
		return createDocumentReview(m.documentService, m.documents, m.src(), m.review, document.Passed, m.textarea.Value())
	case "ctrl+g":
		return createDocumentReview(m.documentService, m.documents, m.src(), m.review, document.Flagged, m.textarea.Value())
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd
}

func (m *documentReviewFormModel) paste(msg tea.PasteMsg) tea.Cmd {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd
}

func (m *documentReviewFormModel) crumbs(message.Screen) []string {
	return []string{"Review " + documentTypeOf(m.review.DocumentType).Label()}
}

func (m *documentReviewFormModel) View() tea.View {
	var b strings.Builder
	if m.changes != "" {
		b.WriteString(m.changesView())
	}
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(documentReviewFormHelp())
	return tea.NewView(b.String())
}

// reviewFailed is a review of one of applicationID's documents that
// wasn't saved.
func reviewFailed(src message.Source, applicationID int64, err error) tea.Cmd {
	return func() tea.Msg { return reviewRefused(src, applicationID, model.ErrorOf(err)) }
}

func reviewRefused(src message.Source, applicationID int64, e message.Error) tea.Msg {
	return message.New(src, message.Subject{Type: message.TypeApplication, ID: applicationID}, message.OpCreated, time.Now().UTC(), e)
}

// createDocumentReview saves a review of d's content, unless the file no
// longer holds it: an agent or $EDITOR may have rewritten it while the
// form was open. A review of a version that's gone would never count as
// current, and an agent would never see its notes, so it's refused with
// document_changed instead.
func createDocumentReview(documentService *service.DocumentService, docs *documents.Store, src message.Source, d message.DocumentReview, outcome document.Outcome, notes string) tea.Cmd {
	return func() tea.Msg {
		fail := func(err error) tea.Msg { return reviewRefused(src, d.ApplicationID, model.ErrorOf(err)) }
		documentType := documentTypeOf(d.DocumentType)
		path, err := docs.Path(d.ApplicationID, documentType)
		if err != nil {
			return fail(err)
		}
		current, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return fail(errors.New("the document was deleted while you were reviewing it, so the review wasn't saved"))
		}
		if err != nil {
			return fail(fmt.Errorf("check the document before saving the review: %w", err))
		}
		if string(current) != d.Content {
			return reviewRefused(src, d.ApplicationID, message.Error{Code: message.ErrDocumentChanged,
				Text: documentChangedText(whoWrote(documentService, d.ApplicationID, documentType, string(current)))})
		}
		review, err := documentService.Review(context.Background(), d.ApplicationID, documentType, d.Content, outcome, notes)
		if err != nil {
			return fail(err)
		}
		return reviewCreated(src, review.CreatedAt, reviewSnapshot(*review))
	}
}

// reviewCreated is r saved. Its Subject is r's application, since that's
// what the screens that show a review show.
func reviewCreated(src message.Source, at time.Time, r message.Review) message.Event[message.Changed[message.Review]] {
	return message.New(src, message.Subject{Type: message.TypeApplication, ID: r.ApplicationID}, message.OpCreated, at, message.Changed[message.Review]{After: r})
}

// documentChangedText says why a review wasn't saved, naming who changed
// the document when Swamp recorded the write.
func documentChangedText(who string) string {
	if who == "" {
		return "the document changed while you were reviewing, so nothing was saved"
	}
	return "the document was " + who + " while you were reviewing, so nothing was saved"
}

// whoWrote describes the recorded write that produced current, e.g.
// "rewritten by the agent at 14:05", or "" if the latest recorded write
// is of some other version (the change was made outside Swamp).
func whoWrote(documentService *service.DocumentService, applicationID int64, documentType document.Type, current string) string {
	write, err := documentService.LatestWrite(context.Background(), applicationID, documentType)
	if errors.Is(err, store.ErrNotFound) {
		return ""
	}
	if err != nil {
		return fmt.Sprintf("rewritten (couldn't tell by whom: %v)", err)
	}
	if write.ContentSHA256 != documents.ContentSHA256(current) {
		return ""
	}
	return "rewritten by " + documentWriterLabel(write.Source) + " at " + localClock(write.WrittenAt)
}

// documentWriterLabel is who a write source is, for a sentence.
func documentWriterLabel(source document.Source) string {
	switch source {
	case document.SourceWriteDocument:
		return "the agent"
	case document.SourceEditor:
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
