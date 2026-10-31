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
// application whose resume is drafted.
func deleteTestApp(t *testing.T) (*App, application.Application) {
	t.Helper()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	target := mustCreateApplication(t, s, posting.ID)

	app := newTestApp(t, s, newTestSyncer(s, nil))
	status, err := app.documents.EnsureDir(target.ID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.Resume).Path, []byte("# Draft\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return app, target
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
	if app.onScreen() != message.ScreenApplicationDelete {
		t.Fatalf("screen after D = %v, want the delete confirmation", app.onScreen())
	}
	return app
}

// resumeDir is the application's document directory.
func resumeDir(t *testing.T, app *App, applicationID int64) string {
	t.Helper()
	return filepath.Dir(mustDoc(t, app.documents.Status(applicationID), document.Resume).Path)
}

func TestDeleteFlow_ConfirmingDeletesTheApplicationAndKeepsItsDocuments(t *testing.T) {
	t.Parallel()
	app, target := deleteTestApp(t)
	app = startDelete(t, app)

	app, cmd := sendKey(app, runeKey('y'))
	app = applyCmd(t, app, cmd)

	if _, err := storeOf(app).Applications.Get(context.Background(), target.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("application.GetByID after confirming: err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(resumeDir(t, app, target.ID)); err != nil {
		t.Errorf("document directory after confirming: Stat err = %v, want it kept", err)
	}
	if app.onScreen() != message.ScreenActiveApplications {
		t.Errorf("screen after confirming = %v, want the home screen", app.onScreen())
	}
	if want := "Deleted the application for Engineer"; app.status != want {
		t.Errorf("status after confirming = %q, want %q", app.status, want)
	}
	if !app.activeApplicationList.Focused() {
		t.Error("the home screen isn't in focus after confirming, so its keys do nothing: detail is still open under it")
	}
	if len(app.activeApplicationList.apps) != 0 {
		t.Errorf("active applications = %d, want the deleted one gone from the list", len(app.activeApplicationList.apps))
	}
}

func TestDeleteFlow_DecliningKeepsTheApplication(t *testing.T) {
	t.Parallel()
	for _, key := range []tea.KeyPressMsg{runeKey('n'), {Code: tea.KeyEsc}, {Code: tea.KeyEnter}} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			app, target := deleteTestApp(t)
			app = startDelete(t, app)

			app, cmd := sendKey(app, key)
			app = applyCmd(t, app, cmd)

			if _, err := storeOf(app).Applications.Get(context.Background(), target.ID); err != nil {
				t.Errorf("application.GetByID after declining: %v, want the application kept", err)
			}
			if !mustDoc(t, app.documents.Status(target.ID), document.Resume).Exists {
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
	app, target := deleteTestApp(t)
	app = startDelete(t, app)
	confirmation := pushed[*applicationDeleteModel](t, app)
	app, _ = sendKey(app, runeKey('y'))

	failure := errors.New("disk on fire")
	app = sendKeyAndApply(t, app, message.New(message.Source{Instance: confirmation.Instance()}, message.Subject{Type: message.TypeApplication, ID: target.ID}, message.OpDeleted, time.Now().UTC(), model.ErrorOf(failure)))

	if app.onScreen() != message.ScreenApplicationDetail {
		t.Errorf("screen = %v, want back on application detail", app.onScreen())
	}
	if app.err == nil || app.err.Error() != failure.Error() {
		t.Errorf("err = %v, want %v shown", app.err, failure)
	}
}

// Nothing is left to delete or to show, so both the confirmation and
// detail close, and the banner says why rather than showing a not found.
func TestDeleteFlow_AlreadyDeletedElsewhere_GoesHomeAndSaysSo(t *testing.T) {
	t.Parallel()

	app, target := deleteTestApp(t)
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = sendKeyAndApply(t, app, runeKey('D'))
	if _, err := storeOf(app).Applications.Delete(context.Background(), target.ID); err != nil {
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

	app, _ := deleteTestApp(t)
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

	app, target := deleteTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	before := strings.SplitN(ansi.Strip(viewText(app)), "\n", 3)[:2]

	app = sendKeyAndApply(t, app, runeKey('d'))

	if app.onScreen() != message.ScreenApplicationDelete {
		t.Fatalf("screen after d = %v, want the delete confirmation", app.onScreen())
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
	if _, err := storeOf(app).Applications.Get(context.Background(), target.ID); err != nil {
		t.Errorf("application.GetByID after n: %v, want the application kept", err)
	}
}

// testDeleteDetail is the application the unit tests confirm deleting.
func testDeleteDetail() message.ApplicationDetail {
	return message.ApplicationDetail{
		Application: message.Application{ID: 1, PostingID: 5, Status: message.ApplicationStarted},
		Posting:     message.Posting{ID: 5, Title: "Engineer", ListingStatus: message.ListingOpen},
		CompanyName: "Acme",
	}
}

// Keeping is the default, so enter can't delete by habit.
func TestApplicationDeleteModel_KeepKeys_AskToClose(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"n", runeKey('n')},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEsc}},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newApplicationDeleteModel(nil, testDeleteDetail(), 80)

			cmd := m.keyPressed(tt.key)
			if cmd == nil {
				t.Fatalf("keyPressed(%s) returned no command, want one asking to close", tt.name)
			}
			if got := cmd(); !pops(got, m.Instance()) {
				t.Errorf("keyPressed(%s) sent %#v, want a pop of the confirmation", tt.name, got)
			}
		})
	}
}

// The confirmation closes once its application is gone, whoever deleted
// it, and when its own delete fails, saying which. A late result from an
// earlier confirmation leaves this one up.
func TestApplicationDeleteModel_ClosesWhenItsApplicationIsDeletedOrItsDeleteFails(t *testing.T) {
	t.Parallel()

	d := testDeleteDetail()
	other := message.Source{Instance: message.NewScreenInstance()}
	failed := func(src message.Source, err error) tea.Msg {
		return message.New(src, message.Subject{Type: message.TypeApplication, ID: d.Application.ID}, message.OpDeleted, time.Now().UTC(), model.ErrorOf(err))
	}
	for _, tt := range []struct {
		name       string
		event      func(m *applicationDeleteModel) tea.Msg
		wantClose  bool
		wantStatus string
	}{
		{name: "its application, deleted by it", event: func(m *applicationDeleteModel) tea.Msg {
			return message.Deleted(m.src(), d.Application.ID, time.Now().UTC(), d.Application)
		}, wantClose: true, wantStatus: "Deleted the application for Engineer"},
		{name: "its application, deleted elsewhere", event: func(*applicationDeleteModel) tea.Msg {
			return message.Deleted(other, d.Application.ID, time.Now().UTC(), d.Application)
		}, wantClose: true, wantStatus: "The application for Engineer was deleted elsewhere"},
		{name: "its delete, finding it already gone", event: func(m *applicationDeleteModel) tea.Msg {
			return failed(m.src(), store.ErrNotFound)
		}, wantClose: true, wantStatus: "The application for Engineer was deleted elsewhere"},
		{name: "its delete, failed", event: func(m *applicationDeleteModel) tea.Msg {
			return failed(m.src(), errors.New("disk full"))
		}, wantClose: true},
		{name: "another application, deleted", event: func(*applicationDeleteModel) tea.Msg {
			return message.Deleted(other, 2, time.Now().UTC(), message.Application{ID: 2, PostingID: 6})
		}},
		{name: "its application, updated", event: func(*applicationDeleteModel) tea.Msg {
			after := d.Application
			after.Status = message.ApplicationInterviewing
			return message.Updated(other, d.Application.ID, time.Now().UTC(), d.Application, after)
		}},
		{name: "an earlier confirmation's delete, failed", event: func(*applicationDeleteModel) tea.Msg {
			return failed(other, errors.New("disk full"))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newApplicationDeleteModel(nil, d, 80)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			got := runCmd(t, r.Dispatch(tt.event(m)))

			if closed := popsIn(got, m.Instance()); closed != tt.wantClose {
				t.Errorf("asked to close = %v, want %v (sent %#v)", closed, tt.wantClose, got)
			}
			if tt.wantStatus != "" && !notifiesIn(got, tt.wantStatus) {
				t.Errorf("sent %#v, want the status %q", got, tt.wantStatus)
			}
		})
	}
}

func TestApplicationDeleteModel_Y_SendsTheApplicationDeletedOnce(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	target := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	d := testDeleteDetail()
	d.Application = applicationSnapshot(target)
	m := newApplicationDeleteModel(s.Applications, d, 80)

	msg := m.keyPressed(runeKey('y'))()

	got, ok := msg.(message.Event[message.Changed[message.Application]])
	if !ok {
		t.Fatalf("y sent %T, want message.Event[message.Changed[message.Application]]", msg)
	}
	assertEventFrom(t, got.Meta, m.src())
	if got.Meta.Op != message.OpDeleted || got.Meta.Subject != (message.Subject{Type: message.TypeApplication, ID: target.ID}) {
		t.Errorf("y sent %v about %+v, want a delete of application %d", got.Meta.Op, got.Meta.Subject, target.ID)
	}
	if again := m.keyPressed(runeKey('y')); again != nil {
		t.Error("a second y sent another delete while the first is in flight")
	}
}

// The confirmation takes keys only while the last focus signal named it.
func TestApplicationDeleteModel_TakesKeysOnlyWhileFocused(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		focused   func(m *applicationDeleteModel) message.ScreenInstance
		wantClose bool
	}{
		{name: "the confirmation in focus", focused: func(m *applicationDeleteModel) message.ScreenInstance { return m.Instance() }, wantClose: true},
		{name: "another screen in focus", focused: func(*applicationDeleteModel) message.ScreenInstance { return message.NewScreenInstance() }},
		{name: "nothing in focus", focused: func(*applicationDeleteModel) message.ScreenInstance { return 0 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newApplicationDeleteModel(nil, testDeleteDetail(), 80)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(focusOn(tt.focused(m)))
			cmd := r.Dispatch(runeKey('n'))

			if closed := cmd != nil && pops(cmd(), m.Instance()); closed != tt.wantClose {
				t.Errorf("n asked to close = %v, want %v", closed, tt.wantClose)
			}
		})
	}
}

func TestApplicationDeleteModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, newApplicationDeleteModel(nil, testDeleteDetail(), 80))
}

func TestDeleteFlow_ConfirmingFromTheHomeList_StaysThereWithoutTheRow(t *testing.T) {
	t.Parallel()

	app, target := deleteTestApp(t)
	app = sendKeyAndApply(t, app, runeKey('d'))

	app = sendKeyAndApply(t, app, runeKey('y'))

	if _, err := storeOf(app).Applications.Get(context.Background(), target.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("application.Get after confirming: err = %v, want ErrNotFound", err)
	}
	if app.onScreen() != message.ScreenActiveApplications || !app.activeApplicationList.Focused() {
		t.Errorf("screen after confirming = %v (home in focus %v), want the home screen in focus", app.onScreen(), app.activeApplicationList.Focused())
	}
	if len(app.activeApplicationList.apps) != 0 {
		t.Errorf("active applications = %d, want the deleted one gone from the list", len(app.activeApplicationList.apps))
	}
	if want := "Deleted the application for Engineer"; app.status != want {
		t.Errorf("status = %q, want %q", app.status, want)
	}
}
