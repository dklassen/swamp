package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

func TestDeleteFlow_ConfirmingDeletesTheApplicationAndItsDocuments(t *testing.T) {
	t.Parallel()
	app, application := deleteTestApp(t)
	app = startDelete(t, app)

	app, cmd := sendKey(app, runeKey('y'))
	app = applyCmd(t, app, cmd)

	if _, err := app.store.GetApplicationByID(context.Background(), application.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetApplicationByID after confirming: err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(resumeDir(t, app, application.ID)); !os.IsNotExist(err) {
		t.Errorf("document directory after confirming: Stat err = %v, want it gone", err)
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
	for _, key := range []tea.KeyMsg{runeKey('n'), {Type: tea.KeyEsc}} {
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
