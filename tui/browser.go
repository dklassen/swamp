package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

// browserCommand picks the OS command to open url in the default browser.
// Split out from openInBrowser as a pure function so the platform-specific
// branching is testable without actually spawning a process.
func browserCommand(goos, url string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "open", []string{url}, nil
	case "linux":
		return "xdg-open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	default:
		return "", nil, fmt.Errorf("tui: don't know how to open a browser on %q", goos)
	}
}

type browserOpenedMsg struct {
	err error
}

func openInBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		cmd, args, err := browserCommand(runtime.GOOS, url)
		if err != nil {
			return browserOpenedMsg{err: err}
		}
		return browserOpenedMsg{err: exec.CommandContext(context.Background(), cmd, args...).Start()}
	}
}
