package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// submitTestApp is an App on the home screen with one started
// application whose cover letter and resume are drafted, a fake browser
// opener recording the URLs it's asked to open, and a temporary export
// directory.
func submitTestApp(t *testing.T, applicationURL, jobURL string) (*App, store.Application, *[]string) {
	t.Helper()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting, err := s.UpsertPosting(ctx, store.CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: store.IngestedFields{
			Title:          "Engineer",
			ApplicationURL: applicationURL,
			JobURL:         jobURL,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	application := mustCreateApplication(t, s, posting.ID)

	app := newTestApp(t, s, newTestSyncer(s, nil))
	paths, err := app.documents.EnsureDir(application.ID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, path := range []string{mustDoc(t, paths, documents.CoverLetter).Path, mustDoc(t, paths, documents.Resume).Path} {
		if err := os.WriteFile(path, []byte("# Draft\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	var opened []string
	app.openURL = func(url string) tea.Cmd {
		opened = append(opened, url)
		return func() tea.Msg { return browserOpenedMsg{} }
	}
	app.exportDir = t.TempDir()
	return app, application, &opened
}

// startSubmit goes from the home screen to the application's detail and
// presses S, running everything the submit screen kicks off.
func startSubmit(t *testing.T, app *App) *App {
	t.Helper()
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if app.screen != screenApplicationDetail {
		t.Fatalf("screen after enter = %v, want application detail", app.screen)
	}
	app, cmd := sendKey(app, runeKey('S'))
	app = applyCmd(t, app, cmd)
	if app.screen != screenApplicationSubmit {
		t.Fatalf("screen after S = %v, want the submit screen", app.screen)
	}
	return app
}

func TestSubmitFlow_OpensApplyLinkExportsAndMarksSubmittedOnConfirm(t *testing.T) {
	t.Parallel()
	app, application, opened := submitTestApp(t, "https://boards.example/job-1/apply", "https://boards.example/job-1")

	app = startSubmit(t, app)

	if diff := cmp.Diff([]string{"https://boards.example/job-1/apply"}, *opened); diff != "" {
		t.Errorf("opened URLs mismatch (-want +got):\n%s", diff)
	}
	for _, name := range []string{"acme-engineer-cover_letter.pdf", "acme-engineer-resume.pdf"} {
		if _, err := os.Stat(filepath.Join(app.exportDir, name)); err != nil {
			t.Errorf("exported %s: %v", name, err)
		}
	}
	if got, err := app.store.GetApplicationByID(context.Background(), application.ID); err != nil || got.Status != store.ApplicationStatusStarted {
		t.Fatalf("status before confirming = %v (err %v), want still started", got.Status, err)
	}

	app, cmd := sendKey(app, runeKey('y'))
	app = applyCmd(t, app, cmd)

	got, err := app.store.GetApplicationByID(context.Background(), application.ID)
	if err != nil {
		t.Fatalf("GetApplicationByID: %v", err)
	}
	if got.Status != store.ApplicationStatusSubmitted {
		t.Errorf("status after confirming = %s, want %s", got.Status, store.ApplicationStatusSubmitted)
	}
	if app.screen != screenActiveApplications {
		t.Errorf("screen after confirming = %v, want the home screen", app.screen)
	}
}

func TestSubmitFlow_DecliningLeavesStatusUnchanged(t *testing.T) {
	t.Parallel()
	for _, key := range []tea.KeyMsg{runeKey('n'), {Type: tea.KeyEsc}} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			app, application, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
			app = startSubmit(t, app)

			app, cmd := sendKey(app, key)
			app = applyCmd(t, app, cmd)

			got, err := app.store.GetApplicationByID(context.Background(), application.ID)
			if err != nil {
				t.Fatalf("GetApplicationByID: %v", err)
			}
			if got.Status != store.ApplicationStatusStarted {
				t.Errorf("status = %s, want it left at %s", got.Status, store.ApplicationStatusStarted)
			}
			if app.screen != screenApplicationDetail {
				t.Errorf("screen = %v, want back on application detail", app.screen)
			}
		})
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

// A browser that fails to open must not be reported as opened: the user
// would wait for a tab that never appears.
func TestSubmitFlow_ReportsALinkThatFailedToOpen(t *testing.T) {
	t.Parallel()
	app, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
	app.openURL = func(string) tea.Cmd {
		return func() tea.Msg { return browserOpenedMsg{err: errors.New("no browser")} }
	}

	app = startSubmit(t, app)

	view := app.applicationSubmit.View()
	if !strings.Contains(view, "couldn't open https://boards.example/job-1/apply") || strings.Contains(view, "opened https://") {
		t.Errorf("View() = %q, want the link reported as not opened", view)
	}
}
