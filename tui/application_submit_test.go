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
// directory, and the services it's over.
func submitTestApp(t *testing.T, applicationURL, jobURL string) (*App, testutil.TestApp, *Screens, application.Application, *[]string) {
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

	app, screens := newTestAppWithScreens(t, s, newTestSyncer(s, nil))
	paths, err := screens.documents.EnsureDir(target.ID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, path := range []string{mustDoc(t, paths, document.CoverLetter).Path, mustDoc(t, paths, document.Resume).Path} {
		if err := os.WriteFile(path, []byte("# Draft\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	var opened []string
	screens.openURL = func(src message.Source, about message.Subject, url string) tea.Cmd {
		opened = append(opened, url)
		return func() tea.Msg { return browserOpened(src, about, url, nil) }
	}
	screens.exportDir = t.TempDir()
	return app, s, screens, target, &opened
}

// startSubmit goes from the home screen to the application's detail and
// presses S, running everything the submit screen kicks off.
func startSubmit(t *testing.T, app *App) *App {
	t.Helper()
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.onScreen() != message.ScreenApplicationDetail {
		t.Fatalf("screen after enter = %v, want application detail", app.onScreen())
	}
	app, cmd := sendKey(app, runeKey('S'))
	app = applyCmd(t, app, cmd)
	if app.onScreen() != message.ScreenApplicationSubmit {
		t.Fatalf("screen after S = %v, want the submit screen", app.onScreen())
	}
	return app
}

func submitView(t *testing.T, app *App) string {
	t.Helper()
	return pushed[*applicationSubmitModel](t, app).View().Content
}

func TestSubmitFlow_OpensApplyLinkExportsAndMarksSubmittedOnConfirm(t *testing.T) {
	t.Parallel()
	app, s, screens, target, opened := submitTestApp(t, "https://boards.example/job-1/apply", "https://boards.example/job-1")

	app = startSubmit(t, app)

	if diff := cmp.Diff([]string{"https://boards.example/job-1/apply"}, *opened); diff != "" {
		t.Errorf("opened URLs mismatch (-want +got):\n%s", diff)
	}
	for _, name := range []string{"acme-engineer-cover_letter.pdf", "acme-engineer-resume.pdf"} {
		if _, err := os.Stat(filepath.Join(screens.exportDir, name)); err != nil {
			t.Errorf("exported %s: %v", name, err)
		}
	}
	if got, err := s.Applications.Get(context.Background(), target.ID); err != nil || got.Status != application.Started {
		t.Fatalf("status before confirming = %v (err %v), want still started", got.Status, err)
	}

	app, cmd := sendKey(app, runeKey('y'))
	app = applyCmd(t, app, cmd)

	got, err := s.Applications.Get(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("application.GetByID: %v", err)
	}
	if got.Status != application.Submitted {
		t.Errorf("status after confirming = %s, want %s", got.Status, application.Submitted)
	}
	if app.onScreen() != message.ScreenApplicationDetail {
		t.Errorf("screen after confirming = %v, want back on application detail", app.onScreen())
	}
	if want := "Marked Engineer submitted"; app.status != want {
		t.Errorf("status line = %q, want %q", app.status, want)
	}
	label := applicationStatusLabel(application.Submitted)
	if view := viewText(app); !strings.Contains(view, label) {
		t.Errorf("application detail = %q, want the new status %q", view, label)
	}

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.onScreen() != message.ScreenActiveApplications {
		t.Fatalf("screen after esc = %v, want the home screen", app.onScreen())
	}
	if view := viewText(app); !strings.Contains(view, label) {
		t.Errorf("home list = %q, want the new status %q", view, label)
	}
}

func TestSubmitFlow_DecliningLeavesStatusUnchanged(t *testing.T) {
	t.Parallel()
	for _, key := range []tea.KeyPressMsg{runeKey('n'), {Code: tea.KeyEsc}} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			app, s, _, target, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
			app = startSubmit(t, app)

			app, cmd := sendKey(app, key)
			app = applyCmd(t, app, cmd)

			got, err := s.Applications.Get(context.Background(), target.ID)
			if err != nil {
				t.Fatalf("application.GetByID: %v", err)
			}
			if got.Status != application.Started {
				t.Errorf("status = %s, want it left at %s", got.Status, application.Started)
			}
			if app.onScreen() != message.ScreenApplicationDetail {
				t.Errorf("screen = %v, want back on application detail", app.onScreen())
			}
		})
	}
}

// A save that lands after the user left and opened submit again belongs
// to the screen they left, so the one open now stays.
func TestSubmitFlow_SaveResolvingAfterReentry_KeepsReopenedScreenOpen(t *testing.T) {
	t.Parallel()
	app, _, _, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
	app = startSubmit(t, app)

	app, saveCmd := sendKey(app, runeKey('y'))
	if saveCmd == nil {
		t.Fatal("Update on y returned nil Cmd, want a command that marks the application submitted")
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc}) // back to application detail, save still in flight
	app, cmd := sendKey(app, runeKey('S'))
	app = applyCmd(t, app, cmd)
	if app.onScreen() != message.ScreenApplicationSubmit {
		t.Fatalf("screen after S again = %v, want the submit screen", app.onScreen())
	}

	app = applyCmd(t, app, saveCmd)

	if app.onScreen() != message.ScreenApplicationSubmit {
		t.Errorf("screen after the earlier instance's save result = %v, want the submit screen (the reopened one stays open)", app.onScreen())
	}
}

// Submit exports to wherever the last export went, so the PDFs it names
// are where the user has been dragging them from.
func TestSubmitFlow_ExportsToTheDirectoryJustExportedTo(t *testing.T) {
	t.Parallel()
	app, _, _, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
	dest := t.TempDir()

	app = sendKeyAndApply(t, app, runeKey('e'))
	pushed[*applicationExportModel](t, app).textinput.SetValue(dest)
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.onScreen() != message.ScreenActiveApplications {
		t.Fatalf("screen after export = %v, want the home screen", app.onScreen())
	}
	app = startSubmit(t, app)

	if view := submitView(t, app); !strings.Contains(view, "Exported 2 PDFs to "+dest) {
		t.Errorf("submit screen = %q, want its export in %s", view, dest)
	}
}

// A failed export is shown on its step, and the screen stays: the user
// still decides whether they submitted.
func TestSubmitFlow_ShowsAFailedExport(t *testing.T) {
	t.Parallel()
	app, _, screens, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	screens.exportDir = filepath.Join(blocker, "sub")

	app = startSubmit(t, app)

	view := submitView(t, app)
	if !strings.Contains(view, "export failed") {
		t.Errorf("submit screen = %q, want the export failure on its step", view)
	}
	if !strings.Contains(view, "opened https://boards.example/job-1/apply") {
		t.Errorf("submit screen = %q, want the link still reported as opened", view)
	}
}

func TestSubmitFlow_FallsBackToJobURL(t *testing.T) {
	t.Parallel()
	app, _, _, _, opened := submitTestApp(t, "", "https://boards.example/job-1")

	startSubmit(t, app)

	if diff := cmp.Diff([]string{"https://boards.example/job-1"}, *opened); diff != "" {
		t.Errorf("opened URLs mismatch (-want +got):\n%s", diff)
	}
}

func TestSubmitFlow_SaysTheLinkOpened(t *testing.T) {
	t.Parallel()
	app, _, _, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")

	app = startSubmit(t, app)

	if view := submitView(t, app); !strings.Contains(view, "opened https://boards.example/job-1/apply") {
		t.Errorf("View() = %q, want the link reported as opened", view)
	}
}

// A browser that fails to open must not be reported as opened: the user
// would wait for a tab that never appears.
func TestSubmitFlow_ReportsALinkThatFailedToOpen(t *testing.T) {
	t.Parallel()
	app, _, screens, _, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
	screens.openURL = func(src message.Source, about message.Subject, url string) tea.Cmd {
		return func() tea.Msg { return browserOpened(src, about, url, errors.New("no browser")) }
	}

	app = startSubmit(t, app)

	view := submitView(t, app)
	if !strings.Contains(view, "couldn't open https://boards.example/job-1/apply") || strings.Contains(view, "opened https://") {
		t.Errorf("View() = %q, want the link reported as not opened", view)
	}
	if strings.Contains(view, "export failed") {
		t.Errorf("View() = %q, want the export unaffected by the browser", view)
	}
}

// A save that fails takes back its y, so the next one tries again rather
// than the screen saying it's marking submitted for good.
func TestSubmitFlow_AFailedSaveLetsYTryAgain(t *testing.T) {
	t.Parallel()
	app, s, _, target, _ := submitTestApp(t, "https://boards.example/job-1/apply", "")
	app = startSubmit(t, app)
	submit := pushed[*applicationSubmitModel](t, app)
	app, _ = sendKey(app, runeKey('y'))

	app = sendKeyAndApply(t, app, applicationWritten(message.Source{Instance: submit.Instance()}, target.ID, message.OpUpdated, nil, errors.New("disk on fire")))

	if view := submitView(t, app); strings.Contains(view, "Marking submitted") {
		t.Errorf("submit screen = %q, want y offered again after the failure", view)
	}
	sendKeyAndApply(t, app, runeKey('y'))
	got, err := s.Applications.Get(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("application.GetByID: %v", err)
	}
	if got.Status != application.Submitted {
		t.Errorf("status after the second y = %s, want %s", got.Status, application.Submitted)
	}
}
