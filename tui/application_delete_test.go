package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// deleteTestApp is an App on the home screen with one started
// application whose resume is drafted.
func deleteTestApp(t *testing.T) (*App, store.Application) {
	t.Helper()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	app := newTestApp(t, s, newTestSyncer(s, nil))
	status, err := app.documents.EnsureDir(application.ID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, documents.Resume).Path, []byte("# Draft\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return app, application
}

// startDelete goes from the home screen to the application's detail,
// looks at its posting and comes back (which records the application in
// applicationsByPosting), and presses D.
func startDelete(t *testing.T, app *App) *App {
	t.Helper()
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if app.screen != screenApplicationDetail {
		t.Fatalf("screen after enter = %v, want application detail", app.screen)
	}
	app, cmd := sendKey(app, runeKey('p'))
	app = applyCmd(t, app, cmd)
	app, cmd = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	app = applyCmd(t, app, cmd)
	if app.screen != screenApplicationDetail {
		t.Fatalf("screen after viewing the posting = %v, want application detail", app.screen)
	}
	if _, ok := app.applicationsByPosting[app.applicationDetail.application.Posting.ID]; !ok {
		t.Fatal("applicationsByPosting has no entry for the application after viewing its posting")
	}
	app, cmd = sendKey(app, runeKey('D'))
	app = applyCmd(t, app, cmd)
	if app.screen != screenApplicationDelete {
		t.Fatalf("screen after D = %v, want the delete confirmation", app.screen)
	}
	return app
}

// resumeDir is the application's document directory.
func resumeDir(t *testing.T, app *App, applicationID int64) string {
	t.Helper()
	return filepath.Dir(mustDoc(t, app.documents.Status(applicationID), documents.Resume).Path)
}

func TestDeleteFlow_ConfirmingDeletesTheApplicationAndKeepsItsDocuments(t *testing.T) {
	t.Parallel()
	app, application := deleteTestApp(t)
	app = startDelete(t, app)

	app, cmd := sendKey(app, runeKey('y'))
	app = applyCmd(t, app, cmd)

	if _, err := app.store.GetApplicationByID(context.Background(), application.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetApplicationByID after confirming: err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(resumeDir(t, app, application.ID)); err != nil {
		t.Errorf("document directory after confirming: Stat err = %v, want it kept", err)
	}
	if app.screen != screenActiveApplications {
		t.Errorf("screen after confirming = %v, want the home screen", app.screen)
	}
	if len(app.activeApplications) != 0 {
		t.Errorf("active applications = %d, want the deleted one gone from the list", len(app.activeApplications))
	}
	if _, ok := app.applicationsByPosting[application.PostingID]; ok {
		t.Error("applicationsByPosting still has the deleted application")
	}
}

func TestDeleteFlow_DecliningKeepsTheApplication(t *testing.T) {
	t.Parallel()
	for _, key := range []tea.KeyMsg{runeKey('n'), {Type: tea.KeyEsc}, {Type: tea.KeyEnter}} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			app, application := deleteTestApp(t)
			app = startDelete(t, app)

			app, cmd := sendKey(app, key)
			app = applyCmd(t, app, cmd)

			if _, err := app.store.GetApplicationByID(context.Background(), application.ID); err != nil {
				t.Errorf("GetApplicationByID after declining: %v, want the application kept", err)
			}
			if !mustDoc(t, app.documents.Status(application.ID), documents.Resume).Exists {
				t.Error("resume gone after declining, want it kept")
			}
			if app.screen != screenApplicationDetail {
				t.Errorf("screen = %v, want back on application detail", app.screen)
			}
		})
	}
}

// A delete that fails goes back to application detail with the error,
// rather than staying on a confirmation whose y is already spent.
func TestDeleteFlow_FailureReturnsToDetailWithTheError(t *testing.T) {
	t.Parallel()
	app, application := deleteTestApp(t)
	app = startDelete(t, app)
	app, _ = sendKey(app, runeKey('y'))

	failure := errors.New("disk on fire")
	app, _ = sendKey(app, applicationDeletedMsg{application: app.applicationDelete.application, err: failure})

	if app.screen != screenApplicationDetail {
		t.Errorf("screen = %v, want back on application detail", app.screen)
	}
	if !errors.Is(app.err, failure) {
		t.Errorf("err = %v, want %v shown", app.err, failure)
	}
	if _, ok := app.applicationsByPosting[application.PostingID]; !ok {
		t.Error("applicationsByPosting dropped an application that wasn't deleted")
	}
}

// TestDeleteFlow_ConfirmationShowsTheStatusAsItIsNow: the confirmation
// describes the application as stored, not as the list loaded it, so you
// don't delete something whose state changed underneath you.
func TestDeleteFlow_ConfirmationShowsTheStatusAsItIsNow(t *testing.T) {
	t.Parallel()

	app, application := deleteTestApp(t)
	if _, err := app.store.UpdateApplicationStatus(context.Background(), application.PostingID, store.ApplicationStatusInterviewing); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	app = startDelete(t, app)

	view := app.applicationDelete.View(120)
	if want := applicationStatusLabel(store.ApplicationStatusInterviewing); !strings.Contains(view, want) {
		t.Errorf("confirmation doesn't show the stored status %q:\n%s", want, view)
	}
}

func TestDeleteFlow_AlreadyDeletedElsewhere_StaysOnDetailAndSaysSo(t *testing.T) {
	t.Parallel()

	app, application := deleteTestApp(t)
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if err := app.store.DeleteApplication(context.Background(), application.ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}

	app = sendKeyAndApply(t, app, runeKey('D'))

	if app.screen != screenApplicationDetail {
		t.Errorf("screen = %v, want application detail (nothing left to confirm)", app.screen)
	}
	if app.err == nil || !strings.Contains(app.err.Error(), "already deleted") {
		t.Errorf("err = %v, want one saying it was already deleted", app.err)
	}
}

// TestDeleteFlow_ConfirmationIsDrawnOverDetail: like the company delete, a
// box over the screen it was opened from, which keeps its header.
func TestDeleteFlow_ConfirmationIsDrawnOverDetail(t *testing.T) {
	t.Parallel()

	app, _ := deleteTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	before := strings.SplitN(ansi.Strip(app.View()), "\n", 3)[:2]

	app = sendKeyAndApply(t, app, runeKey('D'))

	view := ansi.Strip(app.View())
	if after := strings.SplitN(view, "\n", 3)[:2]; !slices.Equal(after, before) {
		t.Errorf("header = %q, want it as before D, %q", after, before)
	}
	if !strings.Contains(view, "Company: Acme") {
		t.Errorf("application detail isn't visible behind the box:\n%s", view)
	}
	if !strings.Contains(view, "y: delete") || !strings.Contains(view, "Delete the application") {
		t.Errorf("no confirmation box over application detail:\n%s", view)
	}
}

// TestDeleteFlow_FromTheHomeList: d on the list opens the same box over
// it, and keeping the application goes back to the list.
func TestDeleteFlow_FromTheHomeList(t *testing.T) {
	t.Parallel()

	app, application := deleteTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	before := strings.SplitN(ansi.Strip(app.View()), "\n", 3)[:2]

	app = sendKeyAndApply(t, app, runeKey('d'))

	if app.screen != screenApplicationDelete {
		t.Fatalf("screen after d = %v, want the delete confirmation", app.screen)
	}
	view := ansi.Strip(app.View())
	if after := strings.SplitN(view, "\n", 3)[:2]; !slices.Equal(after, before) {
		t.Errorf("header = %q, want it as before d, %q", after, before)
	}
	if !strings.Contains(view, "Delete the application for Engineer?") {
		t.Errorf("no confirmation box over the home list:\n%s", view)
	}

	app = sendKeyAndApply(t, app, runeKey('n'))

	if app.screen != screenActiveApplications {
		t.Errorf("screen after n = %v, want the home list", app.screen)
	}
	if _, err := app.store.GetApplicationByID(context.Background(), application.ID); err != nil {
		t.Errorf("GetApplicationByID after n: %v, want the application kept", err)
	}
}
