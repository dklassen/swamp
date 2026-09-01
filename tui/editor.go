package tui

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
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
// returned control to the Program, successfully or not.
type editorClosedMsg struct {
	err error
}

// openInEditor opens path in $EDITOR, taking over the terminal until the
// editor exits (bubbletea suspends its own rendering for the duration --
// see tea.ExecProcess). If $EDITOR isn't set, or the target file doesn't
// exist yet, this still runs: most editors create the file on save, so
// there's nothing to special-case here.
func openInEditor(path string) tea.Cmd {
	cmdName, args, err := editorCommand(os.Getenv("EDITOR"), path)
	if err != nil {
		return func() tea.Msg { return editorClosedMsg{err: err} }
	}
	return tea.ExecProcess(exec.Command(cmdName, args...), func(err error) tea.Msg {
		return editorClosedMsg{err: err}
	})
}
