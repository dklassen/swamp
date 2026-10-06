package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// editorCommand picks the command/args to open path in $EDITOR, given its
// raw env value. Split out from openInEditor as a pure function so the
// decision (and the unset-$EDITOR error) is testable without actually
// spawning a process -- mirrors browserCommand's shape, but this is a
// distinct mechanism from openInBrowser: most editors are terminal
// programs that need tea.ExecProcess (which suspends the Program and
// hands the TTY over) rather than openInBrowser's fire-and-forget
// exec.Command(...).Start() for a detached GUI process.
func editorCommand(editorEnv, path string) (string, []string, error) {
	if editorEnv == "" {
		return "", nil, fmt.Errorf("tui: $EDITOR is not set")
	}
	return editorEnv, []string{path}, nil
}

// editorClosedMsg reports that a tea.ExecProcess-launched $EDITOR has
// returned control to the Program, successfully or not. before is the
// document's hash when the editor opened (empty: it didn't exist), so a
// change can be recorded as the user's.
type editorClosedMsg struct {
	applicationID int64
	documentType  documents.Type
	before        string
	err           error
}

// openInEditor opens path in $EDITOR, taking over the terminal until the
// editor exits (bubbletea suspends its own rendering for the duration --
// see tea.ExecProcess). If $EDITOR isn't set, or the target file doesn't
// exist yet, this still runs: most editors create the file on save, so
// there's nothing to special-case here.
func openInEditor(path string, closed editorClosedMsg) tea.Cmd {
	cmdName, args, err := editorCommand(os.Getenv("EDITOR"), path)
	if err != nil {
		closed.err = err
		return func() tea.Msg { return closed }
	}
	return tea.ExecProcess(exec.Command(cmdName, args...), func(err error) tea.Msg {
		closed.err = err
		return closed
	})
}

// recordEditorWrite records the user's edit if the document changed while
// the editor was open, then reloads the application's reviews, which the
// edit may have made stale.
func recordEditorWrite(s *store.Store, docs *documents.Store, closed editorClosedMsg) tea.Cmd {
	return func() tea.Msg {
		path, err := docs.Path(closed.applicationID, closed.documentType)
		if err != nil {
			return documentReviewsLoadedMsg{applicationID: closed.applicationID, err: err}
		}
		content, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Closed without saving a new document: nothing was written.
		case err != nil:
			return documentReviewsLoadedMsg{applicationID: closed.applicationID, err: fmt.Errorf("read %s after editing: %w", path, err)}
		case documents.ContentSHA256(string(content)) != closed.before:
			if err := s.RecordDocumentWrite(context.Background(), closed.applicationID, closed.documentType, string(content), store.DocumentWriteSourceEditor); err != nil {
				return documentReviewsLoadedMsg{applicationID: closed.applicationID, err: err}
			}
		}
		return loadDocumentReviews(s, docs, closed.applicationID)()
	}
}
