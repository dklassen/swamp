package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
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

// openInEditor opens path in $EDITOR, taking over the terminal until the
// editor exits (bubbletea suspends its own rendering for the duration --
// see tea.ExecProcess), then reports with closed. If the target file
// doesn't exist yet, this still runs: most editors create the file on
// save, so there's nothing to special-case here.
func openInEditor(path string, closed func(error) tea.Msg) tea.Cmd {
	cmdName, args, err := editorCommand(os.Getenv("EDITOR"), path)
	if err != nil {
		return func() tea.Msg { return closed(err) }
	}
	return tea.ExecProcess(exec.Command(cmdName, args...), closed)
}

// editDocument opens one of an application's documents in $EDITOR and,
// once it closes, records the user's write and reports Ran[EditDocument],
// or Error.
func editDocument(documentService *service.DocumentService, docs *documents.Store, src message.Source, applicationID int64, documentType document.Type) tea.Cmd {
	failed := func(err error) tea.Msg { return editFailed(src, applicationID, err) }
	// Most editors create the file on save, but not its directory.
	if _, err := docs.EnsureDir(applicationID); err != nil {
		return func() tea.Msg { return failed(err) }
	}
	path, err := docs.Path(applicationID, documentType)
	if err != nil {
		return func() tea.Msg { return failed(err) }
	}
	before, err := docs.SHA256(applicationID, documentType)
	if err != nil {
		return func() tea.Msg { return failed(err) }
	}
	return openInEditor(path, func(err error) tea.Msg {
		if err != nil {
			return failed(err)
		}
		// Bubble Tea calls this on its event loop; a batch runs the record
		// off it.
		return tea.BatchMsg{recordEditorWrite(documentService, src, applicationID, documentType, path, before)}
	})
}

func editFailed(src message.Source, applicationID int64, err error) tea.Msg {
	return message.New(src, message.Subject{Type: message.TypeApplication, ID: applicationID}, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
}

// recordEditorWrite records the user's edit if the document at path no
// longer has the hash before it had when the editor opened (empty: it
// didn't exist), then reports the edit.
func recordEditorWrite(documentService *service.DocumentService, src message.Source, applicationID int64, documentType document.Type, path, before string) tea.Cmd {
	return func() tea.Msg {
		content, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Closed without saving a new document: nothing was written.
		case err != nil:
			return editFailed(src, applicationID, fmt.Errorf("read %s after editing: %w", path, err))
		case documents.ContentSHA256(string(content)) != before:
			if err := documentService.RecordWrite(context.Background(), applicationID, documentType, string(content), document.SourceEditor); err != nil {
				return editFailed(src, applicationID, err)
			}
		}
		return message.New(src, message.Subject{Type: message.TypeApplication, ID: applicationID}, message.OpNone, time.Now().UTC(),
			message.Ran[message.EditDocument]{Value: message.EditDocument{DocumentType: documentTypeSnapshots[documentType]}})
	}
}
