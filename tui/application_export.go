package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/export"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationExportModel drives the application-export screen: pick a
// destination folder, then generate PDFs of this application's drafted
// documents into it.
//
// The generated PDFs are the hand-off point to the outside world: the
// user drags them from this folder into a job board's upload field, so
// the destination is deliberately theirs to choose rather than fixed at
// the assets directory the way the `swamp export` CLI's is.
type applicationExportModel struct {
	documentService *service.DocumentService
	documents       *documents.Store
	detail          message.ApplicationDetail
	textinput       textinput.Model
	focused         bool
	instance        message.ScreenInstance
}

// newApplicationExportModel returns an export screen for d, with the
// destination prefilled to dir (see App.exportDir) so the common case is
// enter with no typing at all.
func newApplicationExportModel(documentService *service.DocumentService, docs *documents.Store, d message.ApplicationDetail, dir string, size message.ContentSize) *applicationExportModel {
	ti := textinput.New()
	ti.SetValue(dir)
	ti.SetWidth(size.Width)
	// Start the cursor past the prefilled path, so typing a deeper
	// subdirectory extends it instead of landing in front of it.
	ti.CursorEnd()
	ti.Focus()
	return &applicationExportModel{documentService: documentService, documents: docs, detail: d, textinput: ti, instance: message.NewScreenInstance()}
}

func (m *applicationExportModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *applicationExportModel) Instance() message.ScreenInstance { return m.instance }

func (m *applicationExportModel) Focused() bool { return m.focused }

// Routes has no route for its own failed export: the screen stays, since
// the overwhelmingly likely failure is a mistyped destination the user
// can still see and fix, and the banner shows why.
func (m *applicationExportModel) Routes() []*model.Route {
	return []*model.Route{
		model.WhenFocused(m, m.keyPressed),
		model.WhenFocused(m, m.paste),
		model.Always(m.exported, model.From(m.instance)),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *applicationExportModel) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the screen handles is registered.
func (m *applicationExportModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *applicationExportModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

func (m *applicationExportModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.textinput.SetWidth(e.Payload.Value.Width)
	return nil
}

// exported closes the screen, saying where the files went.
func (m *applicationExportModel) exported(e message.Event[message.Ran[message.ExportReport]]) tea.Cmd {
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), exportStatusLine(e.Payload.Value)))
}

func (m *applicationExportModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), ""))
	case "enter":
		return exportApplicationDocuments(m.documentService, m.documents, m.src(), m.detail, m.textinput.Value())
	}
	var cmd tea.Cmd
	m.textinput, cmd = m.textinput.Update(msg)
	return cmd
}

func (m *applicationExportModel) paste(msg tea.PasteMsg) tea.Cmd {
	var cmd tea.Cmd
	m.textinput, cmd = m.textinput.Update(msg)
	return cmd
}

func (m *applicationExportModel) View() tea.View {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Application:") + " " + m.detail.CompanyName + " -- " + m.detail.Posting.Title + "\n\n")
	b.WriteString(model.FieldLabel.Render("Destination folder:") + "\n")
	b.WriteString(m.textinput.View() + "\n")
	b.WriteString(model.HelpStyle.Render("enter: export  esc: cancel"))
	return tea.NewView(b.String())
}

// exportApplicationDocuments renders the application's drafted markdown
// documents to PDFs in dir, which is created if it doesn't exist yet --
// the user typed it as the folder they want the files in, and refusing a
// not-yet-existing desktop subfolder would just send them to a shell to
// mkdir it themselves. It reports a Ran[ExportReport], or an Error.
//
// A document that isn't drafted is skipped, not an error: exporting an
// application whose resume exists but whose cover letter doesn't should
// still produce the resume, matching the `swamp export` CLI's "no
// document on disk, skipped" behavior.
//
// Documents are exported regardless of review outcome. A flagged or
// unreviewed draft can still be worth looking at as a PDF, and the
// active-applications list already shows the review state per
// application, so gating here would hide work without telling the user
// why (same reasoning as the CLI's -- see cmd/swamp/main.go's runExport).
//
// Each PDF written is recorded (DocumentService.RecordExport) with
// the markdown it was rendered from, so the home screen can tell an
// exported draft from one still waiting to be exported.
func exportApplicationDocuments(documentService *service.DocumentService, docs *documents.Store, src message.Source, d message.ApplicationDetail, dir string) tea.Cmd {
	subject := message.Subject{Type: message.TypeApplication, ID: d.Application.ID}
	return func() tea.Msg {
		report, err := exportDocuments(documentService, docs, d, dir)
		if err != nil {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Ran[message.ExportReport]{Value: *report})
	}
}

func exportDocuments(documentService *service.DocumentService, docs *documents.Store, d message.ApplicationDetail, dir string) (*message.ExportReport, error) {
	dir, err := expandPath(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	report := message.ExportReport{Dir: dir}
	status := docs.Status(d.Application.ID)
	for _, documentType := range document.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			return nil, err
		}
		if !doc.Exists {
			report.Skipped = append(report.Skipped, documentTypeSnapshots[documentType])
			continue
		}
		outPath := filepath.Join(dir, export.FileName(d.CompanyName, d.Posting.Title, documentType))
		content, err := export.Document(doc.Path, outPath)
		if err != nil {
			return nil, err
		}
		if err := documentService.RecordExport(context.Background(), d.Application.ID, documentType, content, outPath); err != nil {
			return nil, err
		}
		report.Paths = append(report.Paths, outPath)
	}
	return &report, nil
}

// expandPath resolves a leading "~" in a user-typed destination to the
// home directory. The text input takes a raw string, and "~/Desktop" is
// exactly what a user types there -- without this it would create a
// literal "~" directory under the working directory instead.
func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for %q: %w", path, err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// exportStatusLine describes a finished export for the status bar. It
// names the destination directory explicitly -- the whole point of the
// screen is producing files the user then goes and drags somewhere, so
// "where did they land" is the one thing the confirmation has to say.
func exportStatusLine(r message.ExportReport) string {
	if len(r.Paths) == 0 {
		return "Nothing to export to " + r.Dir + " -- no documents drafted yet"
	}
	noun := "PDFs"
	if len(r.Paths) == 1 {
		noun = "PDF"
	}
	line := fmt.Sprintf("Exported %d %s to %s", len(r.Paths), noun, r.Dir)
	if len(r.Skipped) > 0 {
		labels := make([]string, len(r.Skipped))
		for i, documentType := range r.Skipped {
			labels[i] = documentTypeOf(documentType).Label()
		}
		line += " (skipped " + strings.Join(labels, ", ") + " -- not drafted yet)"
	}
	return line
}
