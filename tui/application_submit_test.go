package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
)

// submitTestApp is an App on the home screen with one started
// application whose cover letter and resume are drafted, a fake browser
// opener recording the URLs it's asked to open, and a temporary export
// directory.
func submitTestApp(t *testing.T, applicationURL, jobURL string) (*App, application.Application, *[]string) {
	t.Helper()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	ingested, err := s.Postings.Ingest(ctx, acme.ID, jobboard.Ashby, "job-1", posting.IngestedFields{
		Title:          "Engineer",
		ApplicationURL: applicationURL,
		JobURL:         jobURL,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	target := mustCreateApplication(t, s, ingested.Posting.ID)

	app := newTestApp(t, s, newTestSyncer(s, nil))
	paths, err := app.documents.EnsureDir(target.ID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, path := range []string{mustDoc(t, paths, document.CoverLetter).Path, mustDoc(t, paths, document.Resume).Path} {
		if err := os.WriteFile(path, []byte("# Draft\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	var opened []string
	app.openURL = func(src message.Source, about message.Subject, url string) tea.Cmd {
		opened = append(opened, url)
		return func() tea.Msg { return browserOpened(src, about, url, nil) }
	}
	app.exportDir = t.TempDir()
	return app, target, &opened
}

// startSubmit goes from the home screen to the application's detail and
// presses S, running everything the submit screen kicks off.
func startSubmit(t *testing.T, app *App) *App {
	t.Helper()
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after enter = %v, want application detail", app.currentScreen)
	}
	app, cmd := sendKey(app, runeKey('S'))
	app = applyCmd(t, app, cmd)
	if app.currentScreen != screenApplicationSubmit {
		t.Fatalf("screen after S = %v, want the submit screen", app.currentScreen)
	}
	return app
}

func TestSubmitFlow_OpensApplyLinkExportsAndMarksSubmittedOnConfirm(t *testing.T) {
	t.Parallel()
	app, target, opened := submitTestApp(t, "https://boards.example/job-1/apply", "https://boards.example/job-1")

	app = startSubmit(t, app)

	if diff := cmp.Diff([]string{"https://boards.example/job-1/apply"}, *opened); diff != "" {
		t.Errorf("opened URLs mismatch (-want +got):\n%s", diff)
	}
	for _, name := range []string{"acme-engineer-cover_letter.pdf", "acme-engineer-resume.pdf"} {
		if _, err := os.Stat(filepath.Join(app.exportDir, name)); err != nil {
			t.Errorf("exported %s: %v", name, err)
		}
	}
	if got, err := storeOf(app).Applications.Get(context.Background(), target.ID); err != nil || got.Status != application.Started {
		t.Fatalf("status before confirming = %v (err %v), want still started", got.Status, err)
	}

	app, cmd := sendKey(app, runeKey('y'))
	app = applyCmd(t, app, cmd)

	got, err := storeOf(app).Applications.Get(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("application.GetByID: %v", err)
	}
	if got.Status != application.Submitted {
		t.Errorf("status after confirming = %s, want %s", got.Status, application.Submitted)
	}
	if app.currentScreen != screenActiveApplications {
		t.Errorf("screen after confirming = %v, want the home screen", app.currentScreen)
	}
	if !app.activeApplicationList.Focused() {
		t.Error("the home screen isn't in focus after confirming, so its keys do nothing: detail is still open under it")
	}
}

func TestSubmitFlow_DecliningLeavesStatusUnchanged(t *testing.T) {
	t.Parallel()
	for _, key := range []tea.KeyPressMsg{runeKey('n'), {Code: tea.KeyEsc}} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			app, target, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
			app = startSubmit(t, app)

			app, cmd := sendKey(app, key)
			app = applyCmd(t, app, cmd)

			got, err := storeOf(app).Applications.Get(context.Background(), target.ID)
			if err != nil {
				t.Fatalf("application.GetByID: %v", err)
			}
			if got.Status != application.Started {
				t.Errorf("status = %s, want it left at %s", got.Status, application.Started)
			}
			if app.currentScreen != screenApplicationDetail {
				t.Errorf("screen = %v, want back on application detail", app.currentScreen)
			}
		})
	}
}

// The submit screen's y saves through the same status message, so it
// needs the same protection.
func TestSubmitFlow_SaveResolvingAfterReentry_KeepsReopenedScreenOpen(t *testing.T) {
	t.Parallel()
	app, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
	app = startSubmit(t, app)

	app, saveCmd := sendKey(app, runeKey('y'))
	if saveCmd == nil {
		t.Fatal("Update on y returned nil Cmd, want a command that marks the application submitted")
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // back to application detail, save still in flight
	app, cmd := sendKey(app, runeKey('S'))
	app = applyCmd(t, app, cmd)
	if app.currentScreen != screenApplicationSubmit {
		t.Fatalf("screen after S again = %v, want the submit screen", app.currentScreen)
	}

	app = applyCmd(t, app, saveCmd)

	if app.currentScreen != screenApplicationSubmit {
		t.Errorf("screen after the earlier instance's save result = %v, want the submit screen (the reopened one stays open)", app.currentScreen)
	}
}

func TestSubmitFlow_FallsBackToJobURL(t *testing.T) {
	t.Parallel()
	app, _, opened := submitTestApp(t, "", "https://boards.example/job-1")

	startSubmit(t, app)

	if diff := cmp.Diff([]string{"https://boards.example/job-1"}, *opened); diff != "" {
		t.Errorf("opened URLs mismatch (-want +got):\n%s", diff)
	}
}

func TestSubmitFlow_SaysTheLinkOpened(t *testing.T) {
	t.Parallel()
	app, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")

	app = startSubmit(t, app)

	if view := app.applicationSubmit.View(); !strings.Contains(view, "opened https://boards.example/job-1/apply") {
		t.Errorf("View() = %q, want the link reported as opened", view)
	}
}

// A browser that fails to open must not be reported as opened: the user
// would wait for a tab that never appears.
func TestSubmitFlow_ReportsALinkThatFailedToOpen(t *testing.T) {
	t.Parallel()
	app, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
	app.openURL = func(src message.Source, about message.Subject, url string) tea.Cmd {
		return func() tea.Msg { return browserOpened(src, about, url, errors.New("no browser")) }
	}

	app = startSubmit(t, app)

	view := app.applicationSubmit.View()
	if !strings.Contains(view, "couldn't open https://boards.example/job-1/apply") || strings.Contains(view, "opened https://") {
		t.Errorf("View() = %q, want the link reported as not opened", view)
	}
}
