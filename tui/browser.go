package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
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

// openInBrowser opens url, the page of about, for src.
func openInBrowser(src message.Source, about message.Subject, url string) tea.Cmd {
	return func() tea.Msg {
		cmd, args, err := browserCommand(runtime.GOOS, url)
		if err == nil {
			err = exec.CommandContext(context.Background(), cmd, args...).Start()
		}
		return browserOpened(src, about, url, err)
	}
}

// browserOpened reports opening url: a Ran, or the Error it failed with.
func browserOpened(src message.Source, about message.Subject, url string, err error) tea.Msg {
	if err != nil {
		return message.New(src, about, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
	}
	return message.New(src, about, message.OpNone, time.Now().UTC(), message.Ran[message.OpenBrowser]{Value: message.OpenBrowser{URL: url}})
}
