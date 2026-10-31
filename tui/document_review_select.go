package tui

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// documentReviewOption is one selectable document on the document-review-
// select screen: its type, resolved path, and whether it exists yet (an
// agent may not have drafted it).
type documentReviewOption struct {
	label        string
	documentType document.Type
	path         string
	exists       bool
}

// documentReviewSelectModel picks which of an application's documents to
// review, and gives way to the review form on it.
type documentReviewSelectModel struct {
	applicationID int64
	options       []documentReviewOption
	cursor        int
	focused       bool
	instance      message.ScreenInstance
}

// newDocumentReviewSelectModel opens on app, with options seeded from its
// documents as they are now.
func newDocumentReviewSelectModel(docs *documents.Store, app message.Application) *documentReviewSelectModel {
	status := docs.Status(app.ID)
	var options []documentReviewOption
	for _, documentType := range document.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			continue
		}
		options = append(options, documentReviewOption{label: documentTitle(documentType), documentType: documentType, path: doc.Path, exists: doc.Exists})
	}
	return &documentReviewSelectModel{applicationID: app.ID, options: options, instance: message.NewScreenInstance()}
}

func (m *documentReviewSelectModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *documentReviewSelectModel) Instance() message.ScreenInstance { return m.instance }

func (m *documentReviewSelectModel) Focused() bool { return m.focused }

func (m *documentReviewSelectModel) Routes() []*model.Route {
	return []*model.Route{
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.focusChanged),
	}
}

func (m *documentReviewSelectModel) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the picker handles is registered.
func (m *documentReviewSelectModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *documentReviewSelectModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

func (m *documentReviewSelectModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "down", "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "esc", "b":
		return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), ""))
	case "enter":
		opt := m.options[m.cursor]
		if !opt.exists {
			return nil
		}
		return openDocumentReview(m.src(), message.Replace[message.DocumentReview], m.applicationID, opt.documentType, opt.path)
	}
	return nil
}

// openDocumentReview opens the review form on the document at path, as
// read now: what's shown is what a review saved from it describes. The
// read is done here rather than in a command, since these are small
// local files. nav is message.Push or message.Replace.
func openDocumentReview(src message.Source, nav func(message.Source, message.Screen, message.Subject, message.DocumentReview) tea.Cmd, applicationID int64, documentType document.Type, path string) tea.Cmd {
	subject := message.Subject{Type: message.TypeApplication, ID: applicationID}
	content, err := os.ReadFile(path)
	if err != nil {
		return func() tea.Msg { return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err)) }
	}
	return nav(src, message.ScreenDocumentReviewForm, subject,
		message.DocumentReview{ApplicationID: applicationID, DocumentType: documentTypeSnapshots[documentType], Content: string(content)})
}

func (m *documentReviewSelectModel) View() tea.View {
	var b strings.Builder
	for i, opt := range m.options {
		status := "not found"
		if opt.exists {
			status = "found"
		}
		line := opt.label + " (" + status + ")"
		if i == m.cursor {
			b.WriteString(model.CursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString(model.HelpStyle.Render("↑/↓ (j/k): select  enter: review  esc/b: cancel"))
	return tea.NewView(b.String())
}
