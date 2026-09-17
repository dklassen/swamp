package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/export"
	"github.com/dklassen/swamp/store"
)

// applicationExportModel drives the application-export screen: pick a
// destination folder, then generate PDFs of this application's drafted
// documents into it. It holds the documents store it needs to resolve
// those drafts, the application it's exporting (fixed for this screen's
// lifetime), and its own private text input -- matching how
// applicationNotesModel and documentReviewFormModel own theirs.
//
// The generated PDFs are the hand-off point to the outside world: the
// user drags them from this folder into a job board's upload field, so
// the destination is deliberately theirs to choose rather than fixed at
// the assets directory the way the `swamp export` CLI's is.
type applicationExportModel struct {
	documents   *documents.Store
	application store.ApplicationView
	textinput   textinput.Model
}

// newApplicationExportModel returns an export screen for application,
// with the destination prefilled to defaultDir (see App.exportDir) so
// the common case is enter with no typing at all.
func newApplicationExportModel(docs *documents.Store, application store.ApplicationView, defaultDir string, width int) applicationExportModel {
	ti := textinput.New()
	ti.SetValue(defaultDir)
	ti.Width = width
	// Start the cursor past the prefilled path, so typing a deeper
	// subdirectory extends it instead of landing in front of it.
	ti.CursorEnd()
	ti.Focus()
	return applicationExportModel{documents: docs, application: application, textinput: ti}
}

// cancelApplicationExportMsg signals that App should switch back to the
// active-applications screen without exporting anything.
type cancelApplicationExportMsg struct{}

func (m *applicationExportModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch msg.Type {
	case tea.KeyEsc:
		return nil, cancelApplicationExportMsg{}
	case tea.KeyEnter:
		return exportApplicationDocuments(m.documents, m.application, m.textinput.Value()), nil
	}
	var cmd tea.Cmd
	m.textinput, cmd = m.textinput.Update(msg)
	return cmd, nil
}

func (m *applicationExportModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Export PDFs") + "\n")
	b.WriteString(fieldLabel.Render("Application:") + " " + m.application.CompanyName + " -- " + m.application.Posting.Title + "\n\n")
	b.WriteString(fieldLabel.Render("Destination folder:") + "\n")
	b.WriteString(m.textinput.View() + "\n")
	b.WriteString(helpStyle.Render("enter: export  esc: cancel"))
	return b.String()
}

// applicationExportedMsg carries the result of one export run: every
// PDF written, and which document types were skipped because they
// haven't been drafted yet. Skipping is deliberately not an error --
// exporting an application whose resume exists but whose cover letter
// doesn't should still produce the resume, matching the `swamp export`
// CLI's "no document on disk, skipped" behavior.
type applicationExportedMsg struct {
	dir     string
	paths   []string
	skipped []store.DocumentType
	err     error
}

// exportApplicationDocuments renders the application's drafted markdown
// documents to PDFs in dir, which is created if it doesn't exist yet --
// the user typed it as the folder they want the files in, and refusing a
// not-yet-existing desktop subfolder would just send them to a shell to
// mkdir it themselves.
//
// Documents are exported regardless of review outcome. A flagged or
// unreviewed draft can still be worth looking at as a PDF, and the
// active-applications list already shows the review state per
// application, so gating here would hide work without telling the user
// why (same reasoning as the CLI's -- see cmd/swamp/main.go's runExport).
func exportApplicationDocuments(docs *documents.Store, application store.ApplicationView, dir string) tea.Cmd {
	return func() tea.Msg {
		dir, err := expandPath(dir)
		if err != nil {
			return applicationExportedMsg{err: err}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return applicationExportedMsg{dir: dir, err: fmt.Errorf("create %s: %w", dir, err)}
		}

		status := docs.Status(application.ID)
		var paths []string
		var skipped []store.DocumentType
		for _, documentType := range []store.DocumentType{store.DocumentTypeCoverLetter, store.DocumentTypeResume} {
			doc := status.CoverLetter
			if documentType == store.DocumentTypeResume {
				doc = status.Resume
			}
			if !doc.Exists {
				skipped = append(skipped, documentType)
				continue
			}
			outPath := filepath.Join(dir, export.FileName(application.CompanyName, application.Posting.Title, documentType))
			if err := export.Document(doc.Path, outPath); err != nil {
				return applicationExportedMsg{dir: dir, paths: paths, skipped: skipped, err: err}
			}
			paths = append(paths, outPath)
		}
		return applicationExportedMsg{dir: dir, paths: paths, skipped: skipped}
	}
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
func exportStatusLine(msg applicationExportedMsg) string {
	if len(msg.paths) == 0 {
		return "Nothing to export to " + msg.dir + " -- no documents drafted yet"
	}
	noun := "PDFs"
	if len(msg.paths) == 1 {
		noun = "PDF"
	}
	line := fmt.Sprintf("Exported %d %s to %s", len(msg.paths), noun, msg.dir)
	if len(msg.skipped) > 0 {
		labels := make([]string, len(msg.skipped))
		for i, documentType := range msg.skipped {
			labels[i] = documentTypeLabel(documentType)
		}
		line += " (skipped " + strings.Join(labels, ", ") + " -- not drafted yet)"
	}
	return line
}
