package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// deleteTestApp is an App on the home screen with one started
// application whose resume is drafted, and the services it's over.
func deleteTestApp(t *testing.T) (*App, testutil.TestApp, application.Application) {
	t.Helper()
	app, s, _, target := deleteTestAppWithScreens(t)
	return app, s, target
}

// deleteTestAppWithScreens is deleteTestApp, with the Screens it opens.
func deleteTestAppWithScreens(t *testing.T) (*App, testutil.TestApp, *Screens, application.Application) {
	t.Helper()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	target := mustCreateApplication(t, s, posting.ID)

	app, screens := newTestAppWithScreens(t, s, newTestSyncer(s, nil))
	status, err := screens.documents.EnsureDir(target.ID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.Resume).Path, []byte("# Draft\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return app, s, screens, target
}

// startDelete goes from the home screen to the application's detail,
// looks at its posting and comes back, and presses D.
func startDelete(t *testing.T, app *App) *App {
	t.Helper()
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.onScreen() != message.ScreenApplicationDetail {
		t.Fatalf("screen after enter = %v, want application detail", app.onScreen())
	}
	app, cmd := sendKey(app, runeKey('p'))
	app = applyCmd(t, app, cmd)
	app, cmd = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	app = applyCmd(t, app, cmd)
	if app.onScreen() != message.ScreenApplicationDetail {
		t.Fatalf("screen after viewing the posting = %v, want application detail", app.onScreen())
	}
	app, cmd = sendKey(app, runeKey('D'))
	app = applyCmd(t, app, cmd)
	if app.onScreen() != message.ScreenApplicationDetail || app.applicationDetail().confirm == nil {
		t.Fatalf("after D: screen %v, want application detail with its delete confirmation open", app.onScreen())
	}
	return app
}

// resumeDir is the application's document directory.
func resumeDir(t *testing.T, screens *Screens, applicationID int64) string {
	t.Helper()
	return filepath.Dir(mustDoc(t, screens.documents.Status(applicationID), document.Resume).Path)
}

func TestDeleteFlow_ConfirmingDeletesTheApplicationAndKeepsItsDocuments(t *testing.T) {
	t.Parallel()
	app, s, screens, target := deleteTestAppWithScreens(t)
	app = startDelete(t, app)

	app, cmd := sendKey(app, runeKey('y'))
	app = applyCmd(t, app, cmd)

	if _, err := s.Applications.Get(context.Background(), target.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("application.GetByID after confirming: err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(resumeDir(t, screens, target.ID)); err != nil {
		t.Errorf("document directory after confirming: Stat err = %v, want it kept", err)
	}
	if app.onScreen() != message.ScreenActiveApplications {
		t.Errorf("screen after confirming = %v, want the home screen", app.onScreen())
	}
	if want := "Deleted the application for Engineer"; app.status != want {
		t.Errorf("status after confirming = %q, want %q", app.status, want)
	}
	if !applicationsTabOf(app).Focused() {
		t.Error("the home screen isn't in focus after confirming, so its keys do nothing: detail is still open under it")
	}
	if len(applicationsTabOf(app).apps) != 0 {
		t.Errorf("active applications = %d, want the deleted one gone from the list", len(applicationsTabOf(app).apps))
	}
}

func TestDeleteFlow_DecliningKeepsTheApplication(t *testing.T) {
	t.Parallel()
	for _, key := range []tea.KeyPressMsg{runeKey('n'), {Code: tea.KeyEsc}, {Code: tea.KeyEnter}} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			app, s, screens, target := deleteTestAppWithScreens(t)
			app = startDelete(t, app)

			app, cmd := sendKey(app, key)
			app = applyCmd(t, app, cmd)

			if _, err := s.Applications.Get(context.Background(), target.ID); err != nil {
				t.Errorf("application.GetByID after declining: %v, want the application kept", err)
			}
			if !mustDoc(t, screens.documents.Status(target.ID), document.Resume).Exists {
				t.Error("resume gone after declining, want it kept")
			}
			if app.onScreen() != message.ScreenApplicationDetail {
				t.Errorf("screen = %v, want back on application detail", app.onScreen())
			}
		})
	}
}

// A delete that fails goes back to application detail with the error,
// rather than staying on a confirmation whose y is already spent.
func TestDeleteFlow_FailureReturnsToDetailWithTheError(t *testing.T) {
	t.Parallel()
	app, _, target := deleteTestApp(t)
	app = startDelete(t, app)
	confirmation := app.applicationDetail().confirm
	app, _ = sendKey(app, runeKey('y'))

	failure := errors.New("disk on fire")
	app = sendKeyAndApply(t, app, message.New(confirmation.src(), message.Subject{Type: message.TypeApplication, ID: target.ID}, message.OpDeleted, time.Now().UTC(), model.ErrorOf(failure)))

	if app.onScreen() != message.ScreenApplicationDetail {
		t.Errorf("screen = %v, want back on application detail", app.onScreen())
	}
	if app.err == nil || app.err.Error() != failure.Error() {
		t.Errorf("err = %v, want %v shown", app.err, failure)
	}
	if app.applicationDetail().confirm != nil {
		t.Error("the confirmation is still open after its delete failed, with its y spent")
	}
}

// Nothing is left to delete or to show, so both the confirmation and
// detail close, and the banner says why rather than showing a not found.
func TestDeleteFlow_AlreadyDeletedElsewhere_GoesHomeAndSaysSo(t *testing.T) {
	t.Parallel()

	app, s, target := deleteTestApp(t)
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = sendKeyAndApply(t, app, runeKey('D'))
	if _, err := s.Applications.Delete(context.Background(), target.ID); err != nil {
		t.Fatalf("application.Delete: %v", err)
	}

	app = sendKeyAndApply(t, app, runeKey('y'))

	if app.onScreen() != message.ScreenActiveApplications {
		t.Errorf("screen = %v, want the home screen (nothing left to show)", app.onScreen())
	}
	if app.err != nil || !strings.Contains(app.status, "was deleted elsewhere") {
		t.Errorf("err, status = %v, %q, want a status saying it was deleted elsewhere", app.err, app.status)
	}
}

// TestDeleteFlow_ConfirmationIsDrawnOverDetail: like the company delete, a
// box over the screen it was opened from, which keeps its header.
func TestDeleteFlow_ConfirmationIsDrawnOverDetail(t *testing.T) {
	t.Parallel()

	app, _, _ := deleteTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	before := strings.SplitN(ansi.Strip(viewText(app)), "\n", 3)[:2]

	app = sendKeyAndApply(t, app, runeKey('D'))

	view := ansi.Strip(viewText(app))
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

	app, s, target := deleteTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	before := strings.SplitN(ansi.Strip(viewText(app)), "\n", 3)[:2]

	app = sendKeyAndApply(t, app, runeKey('d'))

	if app.onScreen() != message.ScreenActiveApplications || applicationsTabOf(app).confirm == nil {
		t.Fatalf("after d: screen %v, want the home list with its delete confirmation open", app.onScreen())
	}
	view := ansi.Strip(viewText(app))
	if after := strings.SplitN(view, "\n", 3)[:2]; !slices.Equal(after, before) {
		t.Errorf("header = %q, want it as before d, %q", after, before)
	}
	if !strings.Contains(view, "Delete the application for Engineer?") {
		t.Errorf("no confirmation box over the home list:\n%s", view)
	}

	app = sendKeyAndApply(t, app, runeKey('n'))

	if app.onScreen() != message.ScreenActiveApplications {
		t.Errorf("screen after n = %v, want the home list", app.onScreen())
	}
	if _, err := s.Applications.Get(context.Background(), target.ID); err != nil {
		t.Errorf("application.GetByID after n: %v, want the application kept", err)
	}
}

func TestDeleteFlow_ConfirmingFromTheHomeList_StaysThereWithoutTheRow(t *testing.T) {
	t.Parallel()

	app, s, target := deleteTestApp(t)
	app = sendKeyAndApply(t, app, runeKey('d'))

	app = sendKeyAndApply(t, app, runeKey('y'))

	if _, err := s.Applications.Get(context.Background(), target.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("application.Get after confirming: err = %v, want ErrNotFound", err)
	}
	if app.onScreen() != message.ScreenActiveApplications || !applicationsTabOf(app).Focused() {
		t.Errorf("screen after confirming = %v (home in focus %v), want the home screen in focus", app.onScreen(), applicationsTabOf(app).Focused())
	}
	if len(applicationsTabOf(app).apps) != 0 {
		t.Errorf("active applications = %d, want the deleted one gone from the list", len(applicationsTabOf(app).apps))
	}
	if want := "Deleted the application for Engineer"; app.status != want {
		t.Errorf("status = %q, want %q", app.status, want)
	}
}

// d opens the confirmation on the row at the cursor, over the list, which
// stays in view behind it.
func TestActiveApplicationListModel_D_OpensTheDeleteConfirmation(t *testing.T) {
	t.Parallel()
	m := homeWith(t, testActiveApplications())
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	if got := pressHome(t, m, runeKey('d')); got != nil {
		t.Errorf("d sent %#v, want nothing: the list opens the confirmation itself", got)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Delete the application for Designer?") || !strings.Contains(view, "y: delete") {
		t.Errorf("no confirmation for the Designer application:\n%s", view)
	}
	if !strings.Contains(view, "enter: application detail") {
		t.Errorf("the list isn't in view behind the confirmation:\n%s", view)
	}
}

// confirmingOwner is a screen with its delete confirmation open on its
// stored application, in focus and registered with r.
type confirmingOwner struct {
	r       *model.MsgRouter
	confirm func() *applicationDeleteModel
	view    func() string
	// next is a key the screen acts on, and took whether it did, given
	// what it sent.
	next tea.KeyPressMsg
	took func(sent []tea.Msg) bool
}

// deleteOwners are the screens that confirm deleting an application.
var deleteOwners = []struct {
	name string
	open func(t *testing.T) confirmingOwner
}{
	{name: "home list", open: func(t *testing.T) confirmingOwner {
		m, r := storedHome(t, true)
		settle(t, r, runCmd(t, m.Init()))
		runCmd(t, r.Dispatch(runeKey('d')))
		return confirmingOwner{r: r, confirm: func() *applicationDeleteModel { return m.confirm },
			view: func() string { return m.View().Content },
			next: runeKey('/'), took: func([]tea.Msg) bool { return m.searching }}
	}},
	{name: "application detail", open: func(t *testing.T) confirmingOwner {
		m, r, _ := applicationDetailWith(t)
		settle(t, r, runCmd(t, m.Init()))
		r.Dispatch(focusOn(m.Instance()))
		runCmd(t, r.Dispatch(runeKey('D')))
		return confirmingOwner{r: r, confirm: func() *applicationDeleteModel { return m.confirm },
			view: func() string { return m.View().Content },
			next: runeKey('b'), took: func(sent []tea.Msg) bool { return popsIn(sent, m.Instance()) }}
	}},
}

// Keeping is the default, so enter can't delete by habit. The key closes
// the confirmation and goes no further.
func TestApplicationDeleteConfirmation_KeepKeysCloseIt(t *testing.T) {
	t.Parallel()
	for _, owner := range deleteOwners {
		for _, key := range []tea.KeyPressMsg{runeKey('n'), {Code: tea.KeyEsc}, {Code: tea.KeyEnter}} {
			t.Run(owner.name+"/"+key.String(), func(t *testing.T) {
				t.Parallel()
				o := owner.open(t)
				if o.confirm() == nil {
					t.Fatal("no confirmation open")
				}

				if got := runCmd(t, o.r.Dispatch(key)); got != nil {
					t.Errorf("%s sent %#v, want nothing", key, got)
				}

				if o.confirm() != nil {
					t.Errorf("the confirmation is still open after %s", key)
				}
			})
		}
	}
}

// The confirmation takes every key while it's open; the first one after
// it closes is its owner's.
func TestApplicationDeleteConfirmation_HoldsItsOwnersKeysUntilItCloses(t *testing.T) {
	t.Parallel()
	for _, owner := range deleteOwners {
		t.Run(owner.name, func(t *testing.T) {
			t.Parallel()
			o := owner.open(t)

			if sent := runCmd(t, o.r.Dispatch(o.next)); o.took(sent) {
				t.Errorf("%s reached the screen with the confirmation open", o.next)
			}

			runCmd(t, o.r.Dispatch(runeKey('n')))
			if sent := runCmd(t, o.r.Dispatch(o.next)); !o.took(sent) {
				t.Errorf("%s after closing didn't reach the screen (sent %#v)", o.next, sent)
			}
		})
	}
}

// The confirmation closes once its application is gone, whoever deleted
// it, and when its own delete fails, saying which. A late failure from an
// earlier confirmation leaves this one up.
func TestApplicationDeleteConfirmation_ClosesWhenItsApplicationIsDeletedOrItsDeleteFails(t *testing.T) {
	t.Parallel()

	other := message.Source{Instance: message.NewScreenInstance()}
	failed := func(src message.Source, id int64, err error) tea.Msg {
		return message.New(src, message.Subject{Type: message.TypeApplication, ID: id}, message.OpDeleted, time.Now().UTC(), model.ErrorOf(err))
	}
	for _, owner := range deleteOwners {
		for _, tt := range []struct {
			name       string
			event      func(c *applicationDeleteModel) tea.Msg
			wantClose  bool
			wantStatus string
		}{
			{name: "its application, deleted by it", event: func(c *applicationDeleteModel) tea.Msg {
				return message.Deleted(c.src(), c.detail.Application.ID, time.Now().UTC(), c.detail.Application)
			}, wantClose: true, wantStatus: "Deleted the application for Engineer"},
			{name: "its application, deleted elsewhere", event: func(c *applicationDeleteModel) tea.Msg {
				return message.Deleted(other, c.detail.Application.ID, time.Now().UTC(), c.detail.Application)
			}, wantClose: true, wantStatus: "The application for Engineer was deleted elsewhere"},
			{name: "its delete, finding it already gone", event: func(c *applicationDeleteModel) tea.Msg {
				return failed(c.src(), c.detail.Application.ID, store.ErrNotFound)
			}, wantClose: true, wantStatus: "The application for Engineer was deleted elsewhere"},
			{name: "its delete, failed", event: func(c *applicationDeleteModel) tea.Msg {
				return failed(c.src(), c.detail.Application.ID, errors.New("disk full"))
			}, wantClose: true},
			{name: "another application, deleted", event: func(c *applicationDeleteModel) tea.Msg {
				id := c.detail.Application.ID + 1
				return message.Deleted(other, id, time.Now().UTC(), message.Application{ID: id})
			}},
			{name: "its application, updated", event: func(c *applicationDeleteModel) tea.Msg {
				after := c.detail.Application
				after.Status = message.ApplicationInterviewing
				return message.Updated(other, c.detail.Application.ID, time.Now().UTC(), c.detail.Application, after)
			}},
			{name: "an earlier confirmation's delete, failed", event: func(c *applicationDeleteModel) tea.Msg {
				return failed(other, c.detail.Application.ID, errors.New("disk full"))
			}},
		} {
			t.Run(owner.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				o := owner.open(t)

				got := runCmd(t, o.r.Dispatch(tt.event(o.confirm())))

				if closed := o.confirm() == nil; closed != tt.wantClose {
					t.Errorf("closed = %v, want %v", closed, tt.wantClose)
				}
				if tt.wantStatus != "" && !notifiesIn(got, tt.wantStatus) {
					t.Errorf("sent %#v, want the status %q", got, tt.wantStatus)
				}
			})
		}
	}
}

// y deletes the application once, from the confirmation, so its result is
// told from an earlier one's; a second y while it's in flight sends
// nothing.
func TestApplicationDeleteConfirmation_Y_DeletesOnce(t *testing.T) {
	t.Parallel()
	for _, owner := range deleteOwners {
		t.Run(owner.name, func(t *testing.T) {
			t.Parallel()
			o := owner.open(t)
			c := o.confirm()

			got := runCmd(t, o.r.Dispatch(runeKey('y')))

			if len(got) != 1 {
				t.Fatalf("y sent %#v, want the delete", got)
			}
			deleted, ok := got[0].(message.Event[message.Changed[message.Application]])
			if !ok {
				t.Fatalf("y sent %T, want message.Event[message.Changed[message.Application]]", got[0])
			}
			assertEventFrom(t, deleted.Meta, c.src())
			if deleted.Meta.Op != message.OpDeleted || deleted.Meta.Subject != (message.Subject{Type: message.TypeApplication, ID: c.detail.Application.ID}) {
				t.Errorf("y sent %v about %+v, want a delete of application %d", deleted.Meta.Op, deleted.Meta.Subject, c.detail.Application.ID)
			}
			if again := runCmd(t, o.r.Dispatch(runeKey('y'))); again != nil {
				t.Errorf("a second y sent %#v, want nothing while the first is in flight", again)
			}
		})
	}
}

// Nothing under the box takes a paste.
func TestApplicationDeleteConfirmation_PasteChangesNothing(t *testing.T) {
	t.Parallel()
	for _, owner := range deleteOwners {
		t.Run(owner.name, func(t *testing.T) {
			t.Parallel()
			o := owner.open(t)
			before := o.view()

			got := runCmd(t, o.r.Dispatch(tea.PasteMsg{Content: "acme"}))

			if got != nil {
				t.Errorf("a paste sent %#v, want nothing", got)
			}
			if after := o.view(); after != before {
				t.Errorf("view changed on a paste with the confirmation open:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}
