package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// newTestApp creates an App and drives it through Init, returning the
// model with its initial companies already loaded. Tests that don't care
// about refresh behavior can pass a syncer with no configured postings.
// The App's documents.Store is rooted at a fresh t.TempDir() -- tests
// that care about it (i.e. application document status) can read it back
// via app.documents.
func newTestApp(t *testing.T, s testutil.TestApp, syncer *sync.Syncer) *App {
	t.Helper()
	app := newAppOn(s, syncer, documents.NewStore(t.TempDir()))
	return applyCmd(t, app, app.Init())
}

// applyCmd runs cmd and feeds the resulting message through app.Update,
// then keeps chasing whatever Cmd that Update call itself returns -- the
// same way a real tea.Program's runtime loop keeps running until nothing
// further is queued. If cmd is itself a tea.Batch (App.Init returns one,
// to load companies and active applications concurrently; several
// posting-detail transitions do too, once loadApplication resolves and
// triggers loadDocumentReviews -- #83), its BatchMsg
// unpacks into per-command messages applied individually. These tests
// drive App directly without a real tea.Program, so this replicates both
// behaviors by hand.
func applyCmd(t *testing.T, app *App, cmd tea.Cmd) *App {
	t.Helper()
	if cmd == nil {
		return app
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			app = applyCmd(t, app, c)
		}
		return app
	}
	model, nextCmd := app.Update(msg)
	return applyCmd(t, model.(*App), nextCmd)
}

// sendKey applies msg to app.Update and returns the resulting model (cast
// back to *App) and any Cmd, saving the repeated type-assertion at every
// call site.
func sendKey(app *App, msg tea.Msg) (*App, tea.Cmd) {
	model, cmd := app.Update(msg)
	return model.(*App), cmd
}

// sendIntentKey is sendKey for a screen whose keys return their intent as
// a command: it delivers that intent too, returning what App does with it.
func sendIntentKey(t *testing.T, app *App, msg tea.Msg) (*App, tea.Cmd) {
	t.Helper()
	app, cmd := sendKey(app, msg)
	if cmd == nil {
		t.Fatalf("Update(%v) returned no intent", msg)
	}
	return sendKey(app, cmd())
}

// viewText is what app shows, without the colour codes Lip Gloss writes
// whether or not there is a terminal to show them.
func viewText(app *App) string {
	return ansi.Strip(app.View().Content)
}

// sendKeyAndApply is sendKey followed by applyCmd, for a key whose screen
// opens only once a load it starts has finished (the status form).
func sendKeyAndApply(t *testing.T, app *App, msg tea.Msg) *App {
	t.Helper()
	app, cmd := sendKey(app, msg)
	return applyCmd(t, app, cmd)
}

func runeKey(r ...rune) tea.KeyPressMsg {
	if len(r) == 1 {
		return tea.KeyPressMsg{Code: r[0], Text: string(r)}
	}
	return tea.KeyPressMsg{Code: tea.KeyExtended, Text: string(r)}
}

// openPostingList drives app from wherever it starts (the active-
// applications home screen) into the posting list for the currently
// selected company: to the company list, refresh, then enter.
func openPostingList(t *testing.T, app *App) *App {
	t.Helper()
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	app, cmd := sendIntentKey(t, app, runeKey('r'))
	if cmd == nil {
		t.Fatal("Update on 'r' returned nil Cmd")
	}
	app, _ = sendKey(app, refreshResult(t, cmd))

	app, cmd = sendIntentKey(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter returned nil Cmd")
	}
	app = applyCmd(t, app, cmd)
	return app
}

// openPostingDetail drives app (already on screenPostingList) into the
// detail view for the currently selected posting, including running the
// command(s) dispatched on entry that load the posting's application (if
// any) and, once that's known, its latest document reviews -- so
// app.applicationsByPosting reflects DB state immediately. Uses applyCmd
// rather than a raw sendKey(app, cmd()) since entry now dispatches a
// tea.Batch of both loads whenever the posting already has an
// application.
func openPostingDetail(t *testing.T, app *App) *App {
	t.Helper()
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	return applyCmd(t, app, cmd)
}

func TestApp_Init_LoadsCompanies(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")

	app := newTestApp(t, s, newTestSyncer(s, nil))

	if len(app.companyList.companies) != 1 {
		t.Fatalf("app.companyList.companies = %+v, want 1 company", app.companyList.companies)
	}
	if app.companyList.companies[0].ID != acme.ID {
		t.Fatalf("app.companyList.companies[0].ID = %d, want %d", app.companyList.companies[0].ID, acme.ID)
	}
}

func TestApp_CursorDown_MovesSelectionWithinBounds(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	if app.companyList.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", app.companyList.cursor)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	if app.companyList.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", app.companyList.cursor)
	}

	// At the bottom, another down should not move past the last company.
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	if app.companyList.cursor != 1 {
		t.Fatalf("cursor after down at bottom = %d, want 1 (clamped)", app.companyList.cursor)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyUp})
	if app.companyList.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", app.companyList.cursor)
	}
}

func TestApp_PressA_EntersCompanyForm(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	if app.currentScreen != screenActiveApplications {
		t.Fatalf("initial screen = %v, want screenActiveApplications (the home screen)", app.currentScreen)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen after 'c' = %v, want screenCompanyList", app.currentScreen)
	}

	app, _ = sendIntentKey(t, app, runeKey('a'))

	if app.currentScreen != screenCompanyForm {
		t.Fatalf("screen after 'a' = %v, want screenCompanyForm", app.currentScreen)
	}
}

func TestApp_SubmitForm_BoardRejectsSourceRef_StaysOnFormToRetry(t *testing.T) {
	s := testutil.NewTestApp(t)
	syncer := sync.New(s.Companies, s.Postings, map[string]sync.PostingFetcher{
		jobboard.Ashby: &fakeFetcher{errBoards: map[string]error{"acmee": errors.New("404 not found")}},
	}, sync.DefaultConfig())
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	app, _ = sendIntentKey(t, app, runeKey('a'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab}) // source (default jobboard.Ashby) -> name
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab}) // name -> sourceRef
	app, _ = sendKey(app, runeKey('a', 'c', 'm', 'e', 'e'))

	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenCompanyForm {
		t.Fatalf("screen after rejected submit = %v, want screenCompanyForm", app.currentScreen)
	}
	if app.err == nil {
		t.Fatal("app.err = nil, want the board check error shown")
	}
	if got := app.companyForm.inputs[formFieldSourceRef].Value(); got != "acmee" {
		t.Fatalf("sourceRef = %q, want %q kept for correction", got, "acmee")
	}
	if _, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter after a rejected submit: cmd = nil, want a retry")
	}
}

func TestApp_TypingInForm_UpdatesFocusedField(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendIntentKey(t, app, runeKey('a'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab}) // source field is focused first; move to name
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))

	if got := app.companyForm.inputs[formFieldName].Value(); got != "Acme" {
		t.Fatalf("companyForm.inputs[formFieldName].Value() = %q, want %q", got, "Acme")
	}
}

func TestApp_Tab_MovesFocusToNextField(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendIntentKey(t, app, runeKey('a'))
	if app.companyForm.focus != formFieldSource {
		t.Fatalf("companyForm.focus on entering the form = %d, want formFieldSource", app.companyForm.focus)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	if app.companyForm.focus != formFieldSourceRef {
		t.Fatalf("companyForm.focus after 2 tabs = %d, want formFieldSourceRef", app.companyForm.focus)
	}
	if !app.companyForm.inputs[formFieldSourceRef].Focused() {
		t.Fatal("companyForm.inputs[formFieldSourceRef] should be focused after 2 tabs")
	}
	if app.companyForm.inputs[formFieldName].Focused() {
		t.Fatal("companyForm.inputs[formFieldName] should be blurred after tabbing away")
	}

	app, _ = sendKey(app, runeKey('a', 'c', 'm', 'e'))
	if got := app.companyForm.inputs[formFieldSourceRef].Value(); got != "acme" {
		t.Fatalf("companyForm.inputs[formFieldSourceRef].Value() = %q, want %q", got, "acme")
	}
}

func TestApp_SubmitForm_CreatesCompanyAndReturnsToList(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendIntentKey(t, app, runeKey('a'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab}) // source (default jobboard.Ashby) -> name
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab}) // name -> sourceRef
	app, _ = sendKey(app, runeKey('a', 'c', 'm', 'e'))

	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that creates the company")
	}
	app, _ = sendKey(app, cmd())

	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen after submit = %v, want screenCompanyList", app.currentScreen)
	}
	if len(app.companyList.companies) != 1 {
		t.Fatalf("app.companyList.companies = %+v, want 1 company", app.companyList.companies)
	}
	if app.companyList.companies[0].Name != "Acme" || app.companyList.companies[0].Source != jobboard.Ashby || app.companyList.companies[0].SourceRef != "acme" {
		t.Fatalf("created company = %+v, want Name=Acme Source=ashby SourceRef=acme", app.companyList.companies[0])
	}

	stored, err := s.Companies.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored companies = %+v, want 1", stored)
	}
}

// TestApp_PressD_AsksBeforeDeletingACompany: deleting closes all its open
// postings, too much for one stray key.
func TestApp_PressD_AsksBeforeDeletingACompany(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Lever, "globex")
	mustCreateCompany(t, s, "Initech", jobboard.Greenhouse, "initech")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	headerBefore := strings.SplitN(ansi.Strip(viewText(app)), "\n", 3)[:2]

	app = sendKeyAndApply(t, app, runeKey('d'))

	if app.currentScreen != screenCompanyDelete {
		t.Errorf("screen after d = %v, want the delete confirmation", app.currentScreen)
	}
	view := viewText(app)
	if !strings.Contains(view, "Delete Acme") || !strings.Contains(view, "y: delete") {
		t.Errorf("the confirmation doesn't name the company and ask:\n%s", view)
	}
	// An overlay: the list it would change stays in view behind it.
	if !strings.Contains(view, "Globex") || !strings.Contains(view, "Initech") {
		t.Errorf("the company list isn't visible behind the confirmation:\n%s", view)
	}
	if header := strings.SplitN(ansi.Strip(view), "\n", 3)[:2]; !slices.Equal(header, headerBefore) {
		t.Errorf("header = %q, want it as before d, %q: the overlay isn't a screen of its own", header, headerBefore)
	}
	if _, err := s.Companies.Get(context.Background(), acme.ID); err != nil {
		t.Errorf("GetCompany after d = %v, want the company still there until confirmed", err)
	}
}

// A result for a confirmation the user closed must not reach the one they
// opened next, so each opening is numbered anew.
func TestApp_CompanyDeleteConfirmation_ReopeningGetsANewInstance(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	app = sendKeyAndApply(t, app, runeKey('d'))
	first := app.companyDelete.Instance()
	app = sendKeyAndApply(t, app, runeKey('n'))

	app = sendKeyAndApply(t, app, runeKey('d'))

	if app.currentScreen != screenCompanyDelete {
		t.Fatalf("screen after the second d = %v, want the delete confirmation", app.currentScreen)
	}
	if second := app.companyDelete.Instance(); second == first || first == 0 || second == 0 {
		t.Errorf("instances of the two openings = %d and %d, want two different nonzero numbers", first, second)
	}
}

// Every model App registers has its own number, so an event's Source
// names exactly one of them.
func TestApp_RegisteredModels_HaveDistinctInstances(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app = sendKeyAndApply(t, app, runeKey('d'))

	instances := map[string]message.ScreenInstance{
		"company list":        app.companyList.Instance(),
		"syncing":             app.syncing.Instance(),
		"delete confirmation": app.companyDelete.Instance(),
	}
	seen := map[message.ScreenInstance]string{}
	for name, instance := range instances {
		if instance == 0 {
			t.Errorf("%s's instance = 0, want a number: 0 is none", name)
		}
		if other, ok := seen[instance]; ok {
			t.Errorf("%s and %s share instance %d", name, other, instance)
		}
		seen[instance] = name
	}
}

// sizeRecorder keeps every content size it's sent.
type sizeRecorder struct {
	instance message.ScreenInstance
	sizes    []message.ContentSize
}

func (r *sizeRecorder) Instance() message.ScreenInstance { return r.instance }

func (r *sizeRecorder) Routes() []*model.Route { return []*model.Route{model.Always(r.resize)} }

func (r *sizeRecorder) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	r.sizes = append(r.sizes, e.Payload.Value)
	return nil
}

// unrelatedMsg is a message no screen handles.
type unrelatedMsg struct{}

// The size is checked on every message, since the banner can change on
// any, but sent only when it differs from the last one sent.
func TestApp_ContentSize_SentOnlyWhenItChanges(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	recorder := &sizeRecorder{instance: message.NewScreenInstance()}
	if err := app.router.Register(recorder); err != nil {
		t.Fatalf("Register: %v", err)
	}

	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = sendKey(app, unrelatedMsg{})
	app, _ = sendKey(app, unrelatedMsg{})

	want := []message.ContentSize{{Width: 100, Rows: app.screenRows()}}
	if diff := cmp.Diff(want, recorder.sizes); diff != "" {
		t.Errorf("sizes sent (-want +got):\n%s", diff)
	}
}

// TestApp_CompanyDeleteConfirmation_AnythingButYKeepsIt: keeping is the
// default, so enter can't delete by habit.
func TestApp_CompanyDeleteConfirmation_AnythingButYKeepsIt(t *testing.T) {
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

			s := testutil.NewTestApp(t)
			for _, name := range []string{"Acme", "Globex", "Initech"} {
				mustCreateCompany(t, s, name, jobboard.Ashby, strings.ToLower(name))
			}
			app := newTestApp(t, s, newTestSyncer(s, nil))
			app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
			app, _ = sendKey(app, runeKey('j'))

			app = sendKeyAndApply(t, app, runeKey('d'))
			app = sendKeyAndApply(t, app, tt.key)

			if app.currentScreen != screenCompanyList {
				t.Errorf("screen after %s = %v, want the company list", tt.name, app.currentScreen)
			}
			if app.companyList.cursor != 1 {
				t.Errorf("cursor after %s = %d, want 1 (where it was)", tt.name, app.companyList.cursor)
			}
			companies, err := s.Companies.ListActive(context.Background())
			if err != nil {
				t.Fatalf("ListActiveCompanies: %v", err)
			}
			if len(companies) != 3 {
				t.Errorf("%d companies after %s, want all 3 kept", len(companies), tt.name)
			}
		})
	}
}

// TestApp_CompanyDeleteConfirmation_TwoQuickNs_CloseItOnce: the second n
// reaches the confirmation before the first one's close does.
func TestApp_CompanyDeleteConfirmation_TwoQuickNs_CloseItOnce(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	app = sendKeyAndApply(t, app, runeKey('d'))

	app, first := sendKey(app, runeKey('n'))
	app, second := sendKey(app, runeKey('n'))
	app = applyCmd(t, app, first)
	app = applyCmd(t, app, second)

	if app.currentScreen != screenCompanyList {
		t.Errorf("screen after n, n = %v, want the company list", app.currentScreen)
	}
	if app.err != nil {
		t.Errorf("err after n, n = %v, want none", app.err)
	}
}

func TestApp_CompanyDeleteConfirmation_HoldsTheListsKeysUntilItCloses(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Lever, "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	app = sendKeyAndApply(t, app, runeKey('d'))

	app = sendKeyAndApply(t, app, runeKey('j'))
	if app.companyList.cursor != 0 {
		t.Errorf("list cursor after j on the confirmation = %d, want 0", app.companyList.cursor)
	}

	app = sendKeyAndApply(t, app, runeKey('n'))
	app = sendKeyAndApply(t, app, runeKey('j'))
	if app.companyList.cursor != 1 {
		t.Errorf("list cursor after closing and j = %d, want 1", app.companyList.cursor)
	}
}

func TestApp_CompanyDeleteFails_ShowsTheErrorOnTheList(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	app = sendKeyAndApply(t, app, runeKey('d'))

	failure := errors.New("disk full")
	app = applyCmd(t, app, func() tea.Msg { return message.CompanyDeletedMsg{CompanyID: acme.ID, Err: failure} })

	if app.currentScreen != screenCompanyList {
		t.Errorf("screen after a failed delete = %v, want the company list", app.currentScreen)
	}
	if !strings.Contains(app.banner(), failure.Error()) {
		t.Errorf("banner after a failed delete = %q, want it to show %q", app.banner(), failure)
	}
}

func TestApp_PressDThenY_DeletesSelectedCompany(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app = sendKeyAndApply(t, app, runeKey('d'))
	app = sendKeyAndApply(t, app, runeKey('y'))

	if app.currentScreen != screenCompanyList {
		t.Errorf("screen after deleting = %v, want the company list", app.currentScreen)
	}
	if len(app.companyList.companies) != 0 {
		t.Fatalf("app.companyList.companies after delete = %+v, want empty (soft-deleted, excluded from active list)", app.companyList.companies)
	}

	_, err := s.Companies.Get(context.Background(), acme.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetCompany after delete error = %v, want ErrNotFound (soft-deleted companies are excluded)", err)
	}
}

func TestApp_PressE_EditsCompanyName(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendIntentKey(t, app, runeKey('e'))
	if app.currentScreen != screenCompanyEdit {
		t.Fatalf("screen after 'e' = %v, want screenCompanyEdit", app.currentScreen)
	}
	if got := app.companyEdit.nameInput.Value(); got != "Acme" {
		t.Fatalf("companyEdit.nameInput.Value() = %q, want %q (seeded from selected company)", got, "Acme")
	}

	app, _ = sendKey(app, runeKey(' ', 'C', 'o', 'r', 'p'))
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that saves the company name")
	}
	app, _ = sendKey(app, cmd())

	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen after submit = %v, want screenCompanyList", app.currentScreen)
	}
	if len(app.companyList.companies) != 1 || app.companyList.companies[0].Name != "Acme Corp" {
		t.Fatalf("app.companyList.companies = %+v, want 1 company named %q", app.companyList.companies, "Acme Corp")
	}
	if app.companyList.companies[0].Source != jobboard.Ashby || app.companyList.companies[0].SourceRef != "acme" {
		t.Fatalf("app.companyList.companies[0] = %+v, want Source/SourceRef unchanged (ashby/acme)", app.companyList.companies[0])
	}

	stored, err := s.Companies.Get(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Name != "Acme Corp" {
		t.Fatalf("stored.Name = %q, want %q", stored.Name, "Acme Corp")
	}
}

func TestApp_EscWhileCompanyNameSavePending_DoesNotRaceTheSave(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendIntentKey(t, app, runeKey('e'))
	app, _ = sendKey(app, runeKey(' ', 'C', 'o', 'r', 'p'))
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that saves the company name")
	}

	// A fast Esc raced against the still-in-flight save from above --
	// bubbletea can't cancel a dispatched command, so this must not
	// switch screens (that would claim "cancelled" while the save is
	// still going to land moments later).
	app, escCmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if escCmd != nil {
		t.Fatalf("Update on esc while save pending returned %v, want nil (esc blocked)", escCmd)
	}
	if app.currentScreen != screenCompanyEdit {
		t.Fatalf("screen after esc while save pending = %v, want screenCompanyEdit (still blocked)", app.currentScreen)
	}

	// The save resolves; screen leaves on its own like a normal submit.
	app, _ = sendKey(app, cmd())
	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen after save resolved = %v, want screenCompanyList", app.currentScreen)
	}

	stored, err := s.Companies.Get(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Name != "Acme Corp" {
		t.Fatalf("stored.Name = %q, want %q (save was not cancelled)", stored.Name, "Acme Corp")
	}
}

func TestApp_SubmitForm_KeepsCompanyListSorted(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	// "Acme" sorts before the already-loaded "Globex" -- appending it
	// blindly would leave the in-memory list out of order even though a
	// fresh load would show it first.
	app, _ = sendIntentKey(t, app, runeKey('a'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab}) // source -> name
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab}) // name -> sourceRef
	app, _ = sendKey(app, runeKey('a', 'c', 'm', 'e'))

	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that creates the company")
	}
	app, _ = sendKey(app, cmd())

	if len(app.companyList.companies) != 2 {
		t.Fatalf("app.companyList.companies = %+v, want 2 companies", app.companyList.companies)
	}
	if app.companyList.companies[0].Name != "Acme" || app.companyList.companies[1].Name != "Globex" {
		t.Fatalf("app.companyList.companies names = [%q, %q], want [Acme, Globex] (alphabetical)",
			app.companyList.companies[0].Name, app.companyList.companies[1].Name)
	}
}

func TestApp_PressE_RenameKeepsCompanyListSorted(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	// Renaming "Acme" (cursor at index 0) to "Zzz" moves it past "Globex"
	// alphabetically -- an in-place replace at the old index would leave
	// the list out of order even though a fresh load would show it last.
	app, _ = sendIntentKey(t, app, runeKey('e'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyBackspace})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyBackspace})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyBackspace})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyBackspace})
	app, _ = sendKey(app, runeKey('Z', 'z', 'z'))

	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that saves the company name")
	}
	app, _ = sendKey(app, cmd())

	if len(app.companyList.companies) != 2 {
		t.Fatalf("app.companyList.companies = %+v, want 2 companies", app.companyList.companies)
	}
	if app.companyList.companies[0].Name != "Globex" || app.companyList.companies[1].Name != "Zzz" {
		t.Fatalf("app.companyList.companies names = [%q, %q], want [Globex, Zzz] (alphabetical)",
			app.companyList.companies[0].Name, app.companyList.companies[1].Name)
	}
}

func TestApp_PressI_OnPostingList_TogglesInterestedOnSelectedPosting(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID

	app, cmd := sendKey(app, runeKey('i'))
	if cmd == nil {
		t.Fatal("Update on 'i' returned nil Cmd, want a command that marks the posting interested")
	}
	app, _ = sendKey(app, cmd())

	markup, err := s.Postings.Markup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("postingmarkup.Get: %v", err)
	}
	if markup.InterestedAt == nil {
		t.Fatal("InterestedAt is nil after pressing 'i', want non-nil")
	}
	if app.postingMarkup[postingID].InterestedAt == nil {
		t.Fatal("app.postingMarkup[postingID].InterestedAt is nil after pressing 'i', want non-nil")
	}

	_, cmd = sendKey(app, runeKey('i'))
	if cmd == nil {
		t.Fatal("Update on second 'i' returned nil Cmd, want a command that unmarks the posting interested")
	}
	_, _ = sendKey(app, cmd())

	markup, err = s.Postings.Markup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("postingmarkup.Get: %v", err)
	}
	if markup.InterestedAt != nil {
		t.Fatal("InterestedAt is non-nil after second 'i', want nil (toggled off)")
	}
}

func TestApp_PressX_OnPostingList_TogglesArchivedOnSelectedPosting(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID

	app, cmd := sendKey(app, runeKey('x'))
	if cmd == nil {
		t.Fatal("Update on 'x' returned nil Cmd, want a command that archives the posting")
	}
	app, _ = sendKey(app, cmd())

	markup, err := s.Postings.Markup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("postingmarkup.Get: %v", err)
	}
	if markup.ArchivedAt == nil {
		t.Fatal("ArchivedAt is nil after pressing 'x', want non-nil")
	}
	if app.postingMarkup[postingID].ArchivedAt == nil {
		t.Fatal("app.postingMarkup[postingID].ArchivedAt is nil after pressing 'x', want non-nil")
	}

	// Archiving hides the posting from view by default, so it's no longer
	// reachable by cursor -- reveal archived postings again before
	// toggling this one back off.
	app, cmd = sendKey(app, runeKey('A'))
	if cmd == nil {
		t.Fatal("Update on 'A' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	_, cmd = sendKey(app, runeKey('x'))
	if cmd == nil {
		t.Fatal("Update on second 'x' returned nil Cmd, want a command that unarchives the posting")
	}
	_, _ = sendKey(app, cmd())

	markup, err = s.Postings.Markup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("postingmarkup.Get: %v", err)
	}
	if markup.ArchivedAt != nil {
		t.Fatal("ArchivedAt is non-nil after second 'x', want nil (toggled off)")
	}
}

func TestApp_PressI_WhileArchived_SwitchesToInterestedAndClearsArchived(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID

	app, cmd := sendKey(app, runeKey('x'))
	app, _ = sendKey(app, cmd())
	if app.postingMarkup[postingID].ArchivedAt == nil {
		t.Fatal("ArchivedAt is nil after pressing 'x', want non-nil (setup)")
	}

	// Archiving hides the posting from view by default, so it's no longer
	// reachable by cursor -- reveal archived postings again before
	// pressing 'i' on it.
	app, cmd = sendKey(app, runeKey('A'))
	if cmd == nil {
		t.Fatal("Update on 'A' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	app, cmd = sendKey(app, runeKey('i'))
	if cmd == nil {
		t.Fatal("Update on 'i' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	markup, err := s.Postings.Markup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("postingmarkup.Get: %v", err)
	}
	if markup.ArchivedAt != nil {
		t.Fatal("ArchivedAt is non-nil after pressing 'i' while archived, want nil (overridden)")
	}
	if markup.InterestedAt == nil {
		t.Fatal("InterestedAt is nil after pressing 'i' while archived, want non-nil")
	}
	if app.postingMarkup[postingID].ArchivedAt != nil {
		t.Fatal("app.postingMarkup[postingID].ArchivedAt is non-nil after pressing 'i' while archived, want nil")
	}
}

func TestApp_PressX_WhileInterested_SwitchesToArchivedAndClearsInterested(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID

	app, cmd := sendKey(app, runeKey('i'))
	app, _ = sendKey(app, cmd())
	if app.postingMarkup[postingID].InterestedAt == nil {
		t.Fatal("InterestedAt is nil after pressing 'i', want non-nil (setup)")
	}

	app, cmd = sendKey(app, runeKey('x'))
	if cmd == nil {
		t.Fatal("Update on 'x' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	markup, err := s.Postings.Markup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("postingmarkup.Get: %v", err)
	}
	if markup.InterestedAt != nil {
		t.Fatal("InterestedAt is non-nil after pressing 'x' while interested, want nil (overridden)")
	}
	if markup.ArchivedAt == nil {
		t.Fatal("ArchivedAt is nil after pressing 'x' while interested, want non-nil")
	}
	if app.postingMarkup[postingID].InterestedAt != nil {
		t.Fatal("app.postingMarkup[postingID].InterestedAt is non-nil after pressing 'x' while interested, want nil")
	}
}

func TestApp_OpenPostingList_HidesArchivedPostingsByDefault(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, cmd := sendIntentKey(t, app, runeKey('r'))
	if cmd == nil {
		t.Fatal("Update on 'r' returned nil Cmd")
	}
	app, _ = sendKey(app, refreshResult(t, cmd))

	postings, err := s.Postings.ListByCompany(context.Background(), app.companyList.companies[0].ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	var archivedID int64
	for _, p := range postings {
		if p.Title == "Designer" {
			archivedID = p.ID
		}
	}
	if archivedID == 0 {
		t.Fatal("could not find Designer posting to archive")
	}
	if _, err := s.Postings.SetArchived(context.Background(), archivedID); err != nil {
		t.Fatalf("postingmarkup.SetArchived: %v", err)
	}

	app, cmd = sendIntentKey(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	if len(app.postings) != 1 {
		t.Fatalf("postings after opening list = %d, want 1 (archived one hidden)", len(app.postings))
	}
	if app.postings[0].Title != "Engineer" {
		t.Fatalf("visible posting = %q, want %q", app.postings[0].Title, "Engineer")
	}
}

func TestApp_PressA_OnPostingList_TogglesArchivedPostingsBackIntoView(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	var archivedID int64
	for _, p := range app.postings {
		if p.Title == "Designer" {
			archivedID = p.ID
		}
	}
	if archivedID == 0 {
		t.Fatal("could not find Designer posting to archive")
	}
	if _, err := s.Postings.SetArchived(context.Background(), archivedID); err != nil {
		t.Fatalf("postingmarkup.SetArchived: %v", err)
	}

	app, cmd := sendKey(app, runeKey('A'))
	if cmd == nil {
		t.Fatal("Update on 'A' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	if len(app.postings) != 2 {
		t.Fatalf("postings after pressing 'A' = %d, want 2 (archived one revealed)", len(app.postings))
	}

	app, cmd = sendKey(app, runeKey('A'))
	if cmd == nil {
		t.Fatal("Update on second 'A' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	if len(app.postings) != 1 {
		t.Fatalf("postings after second 'A' = %d, want 1 (archived one hidden again)", len(app.postings))
	}
}

// A closed listing can't be applied to, so it never belongs in the
// company's posting list. Applications on closed postings stay reachable
// from the active-applications screen.
func TestApp_PostingList_NeverShowsClosedPostings(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	var closedID int64
	for _, p := range app.postings {
		if p.Title == "Designer" {
			closedID = p.ID
		}
	}
	if closedID == 0 {
		t.Fatal("could not find Designer posting to close")
	}
	if err := s.Postings.Close(context.Background(), closedID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Showing archived postings mustn't bring closed ones back.
	app, cmd := sendKey(app, runeKey('A'))
	if cmd == nil {
		t.Fatal("Update on 'A' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	if len(app.postings) != 1 {
		t.Fatalf("postings after reload = %d, want 1 (closed one never shown)", len(app.postings))
	}
	if app.postings[0].Title != "Engineer" {
		t.Fatalf("visible posting = %q, want %q", app.postings[0].Title, "Engineer")
	}
}

func TestApp_PressX_WhileHidingArchived_RemovesPostingFromViewAndClampsCursor(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	if len(app.postings) != 2 {
		t.Fatalf("initial postings = %d, want 2", len(app.postings))
	}
	app.postingList.cursor = 1

	app, cmd := sendKey(app, runeKey('x'))
	if cmd == nil {
		t.Fatal("Update on 'x' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	if len(app.postings) != 1 {
		t.Fatalf("postings after archiving = %d, want 1 (archived one removed immediately)", len(app.postings))
	}
	if app.postingList.cursor != 0 {
		t.Fatalf("postingCursor after archiving = %d, want 0 (clamped)", app.postingList.cursor)
	}
}

func TestApp_PressA_OnPostingDetail_WithNoApplication_CreatesApplication(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	app = openPostingDetail(t, app)

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on 'a' returned nil Cmd, want a command that creates the application")
	}
	_, _ = sendKey(app, cmd())

	got, err := s.Postings.Application(context.Background(), postingID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if got.Status != application.Started {
		t.Fatalf("application.Status = %s, want %s", got.Status, application.Started)
	}
}

// TestApp_PressA_OnPostingDetail_NewApplication_AppearsInActiveApplications
// verifies a freshly-started application shows up in the active-
// applications list without needing a restart -- applicationCreatedMsg
// reloads it, the same way applicationStatusUpdatedMsg already does for
// status changes.
func TestApp_PressA_OnPostingDetail_NewApplication_AppearsInActiveApplications(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	if len(app.activeApplications) != 0 {
		t.Fatalf("app.activeApplications before starting one = %+v, want empty", app.activeApplications)
	}
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on 'a' returned nil Cmd, want a command that creates the application")
	}
	app, createdCmd := sendKey(app, cmd())
	if createdCmd == nil {
		t.Fatal("applicationCreatedMsg handling returned nil Cmd, want a command that reloads active applications")
	}
	app = applyCmd(t, app, createdCmd)

	if len(app.activeApplications) != 1 {
		t.Fatalf("app.activeApplications after starting one = %+v, want 1", app.activeApplications)
	}
}

// TestApp_PressA_OnPostingDetail_ClosedPosting_ExplainsInStatus: starting
// an application on a closed posting is refused. That's an
// expected outcome, not a failure, so it reads as status, not an error.
func TestApp_PressA_OnPostingDetail_ClosedPosting_ExplainsInStatus(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	app = openPostingDetail(t, app)
	if err := s.Postings.Close(context.Background(), postingID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on 'a' returned nil Cmd, want a command that tries to create the application")
	}
	app, _ = sendKey(app, cmd())

	if app.err != nil {
		t.Errorf("app.err = %v, want nil: a closed posting is status, not an error", app.err)
	}
	if !strings.Contains(app.status, "closed") {
		t.Errorf("app.status = %q, want it to say the posting is closed", app.status)
	}
	if _, err := s.Postings.Application(context.Background(), postingID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("application.Get: err = %v, want ErrNotFound", err)
	}
}

func TestApp_PressA_OnPostingDetail_WhenApplicationAlreadyExists_DoesNotDuplicate(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	app = openPostingDetail(t, app)

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on first 'a' returned nil Cmd, want a command that creates the application")
	}
	app, _ = sendKey(app, cmd())

	_, cmd = sendKey(app, runeKey('a'))
	if cmd != nil {
		t.Fatal("Update on second 'a' (application already exists) returned non-nil Cmd, want nil (no-op)")
	}

	applications, err := s.Postings.Application(context.Background(), postingID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if applications.Status != application.Started {
		t.Fatalf("application.Status = %s, want %s (unchanged)", applications.Status, application.Started)
	}
}

func TestApp_OpenPostingDetail_WithExistingApplication_LoadsAndDisplaysStatus(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := createApplicationIn(context.Background(), s, postingID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}

	app = openPostingDetail(t, app)

	got, ok := app.applicationsByPosting[postingID]
	if !ok {
		t.Fatal("app.applicationsByPosting has no entry for postingID after opening detail, want the pre-existing application loaded")
	}
	if got.Status != application.Started {
		t.Fatalf("loaded application.Status = %s, want %s", got.Status, application.Started)
	}
	if !strings.Contains(app.postingDetail.viewport.View(), applicationStatusLabel(application.Started)) {
		t.Fatalf("detail viewport view = %q, want it to contain the application status", app.postingDetail.viewport.View())
	}
}

func TestApp_OpenPostingDetail_WithNoApplication_ShowsNoApplicationMessage(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	if strings.Contains(app.postingDetail.viewport.View(), "application_started") {
		t.Fatalf("detail viewport view = %q, want no application status shown", app.postingDetail.viewport.View())
	}
}

func TestApp_PressS_OnPostingDetail_WithApplication_OpensStatusSelect(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := createApplicationIn(context.Background(), s, postingID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)

	app = sendKeyAndApply(t, app, runeKey('s'))

	if app.currentScreen != screenApplicationStatusSelect {
		t.Fatalf("screen after 's' = %v, want screenApplicationStatusSelect", app.currentScreen)
	}
}

func TestApp_PressS_OnPostingDetail_WithNoApplication_NoOp(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	app = sendKeyAndApply(t, app, runeKey('s'))

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after 's' with no application = %v, want screenPostingDetail (no-op)", app.currentScreen)
	}
}

func TestApp_StatusSelect_Enter_UpdatesStatusAndReturnsToDetail(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := createApplicationIn(context.Background(), s, postingID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)
	app = sendKeyAndApply(t, app, runeKey('s'))

	// Cursor starts at 0 ("application_started"); move down once to land on
	// "application_submitted" (the second entry in applicationStatuses).
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter (status select) returned nil Cmd, want a command that updates the status")
	}
	app, _ = sendKey(app, cmd())

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after saving status = %v, want screenPostingDetail", app.currentScreen)
	}
	if app.applicationsByPosting[postingID].Status != application.Submitted {
		t.Fatalf("app.applicationsByPosting[postingID].Status = %s, want %s", app.applicationsByPosting[postingID].Status, application.Submitted)
	}

	stored, err := s.Postings.Application(context.Background(), postingID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if stored.Status != application.Submitted {
		t.Fatalf("stored application.Status = %s, want %s", stored.Status, application.Submitted)
	}
}

func TestApp_StatusSelect_Esc_CancelsWithoutSaving(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := createApplicationIn(context.Background(), s, postingID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)
	app = sendKeyAndApply(t, app, runeKey('s'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after esc = %v, want screenPostingDetail", app.currentScreen)
	}
	stored, err := s.Postings.Application(context.Background(), postingID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if stored.Status != application.Started {
		t.Fatalf("stored application.Status = %s, want %s (esc should not persist)", stored.Status, application.Started)
	}
}

func TestApp_StartsOnActiveApplicationsScreen(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)

	app := newTestApp(t, s, newTestSyncer(s, nil))

	if app.currentScreen != screenActiveApplications {
		t.Fatalf("initial screen = %v, want screenActiveApplications", app.currentScreen)
	}
	if len(app.activeApplications) != 1 {
		t.Fatalf("app.activeApplications = %+v, want 1 (loaded on Init)", app.activeApplications)
	}
	if app.activeApplications[0].CompanyName != "Acme" {
		t.Fatalf("activeApplications[0].CompanyName = %q, want %q", app.activeApplications[0].CompanyName, "Acme")
	}
}

// TestApp_ActiveApplications_StatusChange_ReturnsToActiveApplications
// exercises the return-screen tracking returnStack exists for: screenApplicationStatusSelect is reachable from both
// screenPostingDetail and screenActiveApplications now, so saving (or
// cancelling) must land back on whichever one entered it, not a
// hardcoded screen.
func TestApp_ActiveApplications_StatusChange_ReturnsToActiveApplications(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app = sendKeyAndApply(t, app, runeKey('s'))
	if app.currentScreen != screenApplicationStatusSelect {
		t.Fatalf("screen after 's' = %v, want screenApplicationStatusSelect", app.currentScreen)
	}

	// Move from "application_started" (index 0) to "application_submitted".
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter (status select) returned nil Cmd, want a command that updates the status")
	}
	app, _ = sendKey(app, cmd())

	if app.currentScreen != screenActiveApplications {
		t.Fatalf("screen after saving status = %v, want screenActiveApplications (where 's' was pressed from)", app.currentScreen)
	}
}

// TestApp_ActiveApplications_StatusChangeToRejected_RemovesFromList
// verifies the applicationStatusUpdatedMsg handler reloads the active
// list after a status change, since moving to a terminal status
// (rejected/offer_declined) should drop the application from view
// immediately rather than on the next full reload.
func TestApp_ActiveApplications_StatusChangeToRejected_RemovesFromList(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app = sendKeyAndApply(t, app, runeKey('s'))
	// Move from "application_started" (index 0) to "rejected" (index 3 --
	// started, submitted, interviewing, rejected).
	for range 3 {
		app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter (status select) returned nil Cmd, want a command that updates the status")
	}
	app, statusCmd := sendKey(app, cmd())
	if statusCmd == nil {
		t.Fatal("applicationStatusUpdatedMsg handling returned nil Cmd, want a command that reloads active applications")
	}
	app, _ = sendKey(app, statusCmd())

	if len(app.activeApplications) != 0 {
		t.Fatalf("app.activeApplications after rejecting = %+v, want empty", app.activeApplications)
	}
}

func TestApp_PressN_OnPostingDetail_WithApplication_OpensNotesEditorPrepopulated(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	created, err := createApplicationIn(context.Background(), s, postingID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	if _, err := s.Applications.SetNotes(context.Background(), created.ID, "Referred by Alice"); err != nil {
		t.Fatalf("application.UpdateNotes: %v", err)
	}
	app = openPostingDetail(t, app)

	app, _ = sendKey(app, runeKey('n'))

	if app.currentScreen != screenApplicationNotesEdit {
		t.Fatalf("screen after 'n' = %v, want screenApplicationNotesEdit", app.currentScreen)
	}
	if got := app.applicationNotes.textarea.Value(); got != "Referred by Alice" {
		t.Fatalf("applicationNotes.textarea.Value() = %q, want %q", got, "Referred by Alice")
	}
}

func TestApp_NotesEdit_CtrlS_SavesNotesAndReturnsToDetail(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := createApplicationIn(context.Background(), s, postingID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('n'))

	app, _ = sendKey(app, runeKey('H', 'i'))
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update on ctrl+s (notes edit) returned nil Cmd, want a command that saves notes")
	}
	app, _ = sendKey(app, cmd())

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after saving notes = %v, want screenPostingDetail", app.currentScreen)
	}
	if app.applicationsByPosting[postingID].Notes != "Hi" {
		t.Fatalf("app.applicationsByPosting[postingID].Notes = %q, want %q", app.applicationsByPosting[postingID].Notes, "Hi")
	}

	stored, err := s.Postings.Application(context.Background(), postingID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if stored.Notes != "Hi" {
		t.Fatalf("stored application.Notes = %q, want %q", stored.Notes, "Hi")
	}
}

func TestApp_NotesEdit_Esc_CancelsWithoutSaving(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := createApplicationIn(context.Background(), s, postingID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('n'))

	app, _ = sendKey(app, runeKey('H', 'i'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	app, _ = sendKey(app, runeKey('y')) // discard the unsaved "Hi"

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after esc and discarding = %v, want screenPostingDetail", app.currentScreen)
	}
	stored, err := s.Postings.Application(context.Background(), postingID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if stored.Notes != "" {
		t.Fatalf("stored application.Notes = %q, want empty (esc should not persist)", stored.Notes)
	}
}

func TestApp_PressN_OnPostingDetail_WithNoApplication_NoOp(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	app, _ = sendKey(app, runeKey('n'))

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after 'n' with no application = %v, want screenPostingDetail (no-op)", app.currentScreen)
	}
}

func TestApp_PressA_OnPostingDetail_CreatedApplication_ReflectedInDetailView(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)
	if strings.Contains(app.postingDetail.viewport.View(), "application_started") {
		t.Fatal("detail view shows application_started before creating an application")
	}

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on 'a' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	if !strings.Contains(app.postingDetail.viewport.View(), applicationStatusLabel(application.Started)) {
		t.Fatalf("detail view after creating application = %q, want it to contain the new status", app.postingDetail.viewport.View())
	}
}

func TestApp_PostingDetail_NavigatingBetweenPostings_LoadsEachPostingsOwnApplication(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	engineerID := app.postings[0].ID
	designerID := app.postings[1].ID
	if _, err := createApplicationIn(context.Background(), s, designerID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	if _, err := updateApplicationStatusIn(context.Background(), s, designerID, application.Interviewing); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}
	app = openPostingDetail(t, app) // opens the posting at cursor 0, no application

	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyRight})
	if cmd == nil {
		t.Fatal("Update on right (posting detail nav) returned nil Cmd, want a command that loads the next posting's application")
	}
	app = applyCmd(t, app, cmd)

	if !strings.Contains(app.postingDetail.viewport.View(), applicationStatusLabel(application.Interviewing)) {
		t.Fatalf("detail view for Designer = %q, want it to contain %q", app.postingDetail.viewport.View(), applicationStatusLabel(application.Interviewing))
	}
	if _, ok := app.applicationsByPosting[engineerID]; ok {
		t.Fatal("applicationsByPosting has an entry for Engineer, want none (it has no application)")
	}
}

func TestApp_PressQ_ReturnsQuitCmd(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	_, cmd := sendKey(app, runeKey('q'))
	if cmd == nil {
		t.Fatal("Update on 'q' returned nil Cmd, want tea.Quit")
	}

	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("cmd() = %T, want tea.QuitMsg", quitMsg)
	}
}

func TestApp_PressEsc_CancelsFormBackToList(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen after esc = %v, want screenCompanyList", app.currentScreen)
	}
	if len(app.companyList.companies) != 0 {
		t.Fatalf("app.companyList.companies after cancel = %+v, want empty (nothing submitted)", app.companyList.companies)
	}
}

func TestApp_CancellingAScreen_ClearsStaleError(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendIntentKey(t, app, runeKey('a'))
	// Simulate an error left over from a failed action on this screen
	// (e.g. a save that failed) -- a real failure would set this via a
	// tea.Msg, but the point under test is what happens to it on the
	// next screen transition, not how it got set.
	app.err = errors.New("boom")

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen after esc = %v, want screenCompanyList", app.currentScreen)
	}
	if app.err != nil {
		t.Fatalf("app.err after cancelling out = %v, want nil (stale error should clear on screen transition)", app.err)
	}
}

func TestApp_PressR_RefreshesSelectedCompanyAndShowsStatus(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, cmd := sendIntentKey(t, app, runeKey('r'))
	if cmd == nil {
		t.Fatal("Update on 'r' returned nil Cmd, want a command that refreshes the company")
	}
	app = sendKeyAndApply(t, app, refreshResult(t, cmd))

	if app.status == "" {
		t.Fatal("app.status should be set after refresh")
	}
	if app.err != nil {
		t.Fatalf("app.err = %v, want nil", app.err)
	}

	postings, err := s.Postings.ListByCompany(context.Background(), app.companyList.companies[0].ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("stored postings = %+v, want 1", postings)
	}
}

// TestApp_PressR_WhileCompanySyncsElsewhere_ShowsStatusNotError: when
// another sync -- e.g. a scheduled `swamp fetch` -- holds the company's
// sync lease, 'r' says so plainly instead of showing a generic error
// .
func TestApp_PressR_WhileCompanySyncsElsewhere_ShowsStatusNotError(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	if taken, err := s.Companies.AcquireSyncLease(context.Background(), acme.ID, "swamp-fetch", time.Minute); err != nil || !taken {
		t.Fatalf("AcquireSyncLease = %v, %v", taken, err)
	}
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{"acme": {{SourceID: "job-1", Title: "Engineer"}}})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, cmd := sendIntentKey(t, app, runeKey('r'))
	app = sendKeyAndApply(t, app, refreshResult(t, cmd))

	if app.err != nil {
		t.Errorf("app.err = %v, want nil -- another sync running isn't a failure", app.err)
	}
	if want := "Acme is already syncing"; !strings.Contains(app.status, want) {
		t.Errorf("app.status = %q, want it to contain %q", app.status, want)
	}
}

func TestApp_PressEnter_OpensPostingListForSelectedCompany(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering"}},
	})
	app := newTestApp(t, s, syncer)

	app = openPostingList(t, app)

	if app.currentScreen != screenPostingList {
		t.Fatalf("screen after enter = %v, want screenPostingList", app.currentScreen)
	}
	if len(app.postings) != 1 {
		t.Fatalf("app.postings = %+v, want 1", app.postings)
	}
	if app.postings[0].Title != "Engineer" {
		t.Fatalf("app.postings[0].Title = %q, want %q", app.postings[0].Title, "Engineer")
	}
	if app.selectedCompany.ID != acme.ID {
		t.Fatalf("app.selectedCompany.ID = %d, want %d", app.selectedCompany.ID, acme.ID)
	}
}

func TestApp_PressEsc_OnPostingList_ReturnsToCompanyList(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen after esc = %v, want screenCompanyList", app.currentScreen)
	}
}

func TestApp_PostingListCursor_MovesWithinBounds(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	if app.postingList.cursor != 0 {
		t.Fatalf("initial postingCursor = %d, want 0", app.postingList.cursor)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	if app.postingList.cursor != 1 {
		t.Fatalf("postingCursor after down = %d, want 1", app.postingList.cursor)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	if app.postingList.cursor != 1 {
		t.Fatalf("postingCursor after down at bottom = %d, want 1 (clamped)", app.postingList.cursor)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyUp})
	if app.postingList.cursor != 0 {
		t.Fatalf("postingCursor after up = %d, want 0", app.postingList.cursor)
	}
}

func TestApp_PressEnter_OnPostingList_OpensDetailForSelectedPosting(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app = openPostingDetail(t, app)

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after enter on posting list = %v, want screenPostingDetail", app.currentScreen)
	}
}

func TestApp_PressEsc_OnPostingDetail_ReturnsToPostingList(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.currentScreen != screenPostingList {
		t.Fatalf("screen after esc on detail = %v, want screenPostingList", app.currentScreen)
	}
}

func TestApp_PostingDetail_RightMovesToNextPostingStayingInDetail(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	// ListPostingsByCompany orders first_seen_at DESC, id DESC -- both
	// postings share a first_seen_at (one sync batch), so the
	// later-inserted job-2/Designer (higher id) sorts first. Entering
	// detail from the top of the list starts on Designer.
	if app.postingDetail.posting.Title != "Designer" {
		t.Fatalf("initial posting shown in detail = %q, want %q", app.postingDetail.posting.Title, "Designer")
	}

	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyRight})

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after right on detail = %v, want screenPostingDetail (stay in detail)", app.currentScreen)
	}
	if app.postingDetail.posting.Title != "Engineer" {
		t.Fatalf("posting shown after right on detail = %q, want %q", app.postingDetail.posting.Title, "Engineer")
	}
	if cmd == nil {
		t.Fatal("Update on right returned nil Cmd, want a command that loads the new posting's application")
	}
	app, _ = sendKey(app, cmd())

	// Clamped at the last posting.
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyRight})
	if app.postingDetail.posting.Title != "Engineer" {
		t.Fatalf("posting shown after right at last posting = %q, want %q (clamped)", app.postingDetail.posting.Title, "Engineer")
	}

	app, cmd = sendKey(app, tea.KeyPressMsg{Code: tea.KeyLeft})
	if app.postingDetail.posting.Title != "Designer" {
		t.Fatalf("posting shown after left on detail = %q, want %q", app.postingDetail.posting.Title, "Designer")
	}
	if cmd == nil {
		t.Fatal("Update on left returned nil Cmd, want a command that loads the new posting's application")
	}
	app, _ = sendKey(app, cmd())

	// Returning to the list should land the cursor on the posting last
	// viewed in detail, not wherever it was before entering detail.
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.postingList.cursor != 0 {
		t.Fatalf("postingList.cursor after returning from detail = %d, want 0 (synced to last-viewed posting)", app.postingList.cursor)
	}
}

func TestApp_PostingDetail_DownScrollsLongDescription(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	longDescription := strings.Repeat("line\n", 100)
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longDescription}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	if app.postingDetail.viewport.YOffset() != 0 {
		t.Fatalf("initial YOffset = %d, want 0", app.postingDetail.viewport.YOffset())
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})

	if app.postingDetail.viewport.YOffset() == 0 {
		t.Fatal("YOffset after down on a long description should have scrolled past 0")
	}
	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after down on detail = %v, want screenPostingDetail", app.currentScreen)
	}
}

// TestApp_PostingDetail_FitsTheTerminalUnderTheBanner checks the whole
// rendered App -- the status/error banner View() draws below every screen,
// plus posting detail itself -- fits in the terminal, so the posting title
// isn't pushed off the screen. openPostingList syncs the company first, which
// leaves a status line up; a failed browser open replaces it with an
// error, after posting detail is already on screen.
func TestApp_PostingDetail_FitsTheTerminalUnderTheBanner(t *testing.T) {
	const width, height = 80, 24
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{name: "status", msg: nil},
		{name: "error", msg: browserOpenedMsg{err: errors.New("could not open browser")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testutil.NewTestApp(t)
			mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			syncer := newTestSyncer(s, map[string][]jobboard.Posting{
				"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: strings.Repeat("line\n", 100)}},
			})
			app := newTestApp(t, s, syncer)
			app, _ = sendKey(app, tea.WindowSizeMsg{Width: width, Height: height})
			app = openPostingList(t, app)
			app = openPostingDetail(t, app)
			if tt.msg != nil {
				app, _ = sendKey(app, tt.msg)
			}

			// One row per line: bubbletea truncates lines wider than the
			// window rather than wrapping them.
			view := ansi.Strip(viewText(app))
			if rows := strings.Count(view, "\n") + 1; rows > height {
				t.Errorf("view takes %d terminal rows, want at most %d:\n%s", rows, height, view)
			}
		})
	}
}

func TestApp_PostingDetail_VimJ_ScrollsLikeDown(t *testing.T) {
	// bubbles/viewport's DefaultKeyMap already binds j/k/h/l alongside the
	// arrow keys, so this should work with no changes on our side --
	// verifying that rather than assuming it.
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	longDescription := strings.Repeat("line\n", 100)
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longDescription}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	app, _ = sendKey(app, runeKey('j'))

	if app.postingDetail.viewport.YOffset() == 0 {
		t.Fatal("YOffset after 'j' on a long description should have scrolled past 0")
	}
}

func TestApp_PostingDetail_ApplicationExistsWithFiles_ShowsFoundStatus(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	// Wide enough that the documents lines (which embed a full t.TempDir()
	// path) aren't wrapped across lines -- wrapping itself is covered by
	// other detail-view tests, not the concern here.
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)

	posting := app.postings[0]
	application, err := createApplicationIn(context.Background(), s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("# Cover Letter"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.Resume).Path, []byte("# Resume"), 0o644); err != nil {
		t.Fatalf("WriteFile resume: %v", err)
	}

	app = openPostingDetail(t, app)

	view := viewText(app)
	if !strings.Contains(view, "found ("+mustDoc(t, status, document.CoverLetter).Path+")") {
		t.Errorf("view does not show cover letter found with path %q:\n%s", mustDoc(t, status, document.CoverLetter).Path, view)
	}
	if !strings.Contains(view, "found ("+mustDoc(t, status, document.Resume).Path+")") {
		t.Errorf("view does not show resume found with path %q:\n%s", mustDoc(t, status, document.Resume).Path, view)
	}
}

func TestApp_PostingDetail_DocumentHasReview_ShowsOutcomeAndNotesInline(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)

	posting := app.postings[0]
	application, err := createApplicationIn(context.Background(), s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	// Content must match what's on disk, or the staleness check (see
	// document.Review.IsCurrent) treats this review
	// as not describing the current document and omits it.
	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("Dear hiring manager..."), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}
	if _, err := createReview(context.Background(), s, application.ID, document.CoverLetter, "Dear hiring manager...", document.Flagged, "too generic, mention Go specifically"); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	app = openPostingDetail(t, app)

	view := viewText(app)
	if !strings.Contains(view, "[FLAGGED]") {
		t.Errorf("view does not show cover letter review outcome [FLAGGED]:\n%s", view)
	}
	if !strings.Contains(view, "too generic, mention Go specifically") {
		t.Errorf("view does not show cover letter review notes:\n%s", view)
	}
	if !strings.Contains(view, "[not reviewed]") {
		t.Errorf("view does not show resume as [not reviewed] (no review recorded for it):\n%s", view)
	}
}

// TestApp_PostingDetail_StaleReview_ShowsNotReviewedNotOldFlag verifies
// the fix for a gap the user caught: once a flagged document is revised
// (its content on disk changes) without a fresh review being recorded,
// the old flagged review no longer describes the current draft and
// should stop rendering as if it did -- posting detail should show
// "[not reviewed]" for the revised document, not the stale "[FLAGGED]"
// and its now-out-of-date notes (see
// document.Review.IsCurrent).
// TestApp_LoadActiveApplications_DocumentReadFails_SetsErr is a
// regression test caught in code review: documents.Current used to
// swallow an os.ReadFile error and silently treat the document as "not
// reviewed" -- indistinguishable from a genuinely stale review, and
// capable of rendering a real, unaddressed [FLAGGED] document as
// [not reviewed] with no indication anything went wrong. A read failure
// must now set app.err instead.
func TestApp_LoadActiveApplications_DocumentReadFails_SetsErr(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application, err := createApplicationIn(context.Background(), s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app := newAppOn(s, newTestSyncer(s, nil), documents.NewStore(t.TempDir()))
	if _, err := createReview(context.Background(), s, application.ID, document.CoverLetter, "letter", document.Flagged, "needs work"); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A directory at the cover letter's path: os.Stat succeeds (Exists
	// reports true, same as a real file) but os.ReadFile fails with "is
	// a directory" -- simulates a genuine read failure portably.
	if err := os.Mkdir(mustDoc(t, status, document.CoverLetter).Path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	app = applyCmd(t, app, app.Init())

	if app.err == nil {
		t.Fatal("app.err = nil, want an error -- a document read failure must not be silently treated as \"not reviewed\"")
	}
}

// Init's loads finish in any order, so one's success mustn't clear
// another's failure.
func TestApp_LoadSucceedsAfterAnotherFails_KeepsTheError(t *testing.T) {
	t.Parallel()
	loadErr := errors.New("disk on fire")
	tests := []struct {
		name    string
		failed  tea.Msg
		succeed tea.Msg
	}{
		{"applications fail, companies succeed", activeApplicationsLoadedMsg{err: loadErr}, message.CompaniesLoadedMsg{}},
		{"companies fail, applications succeed", message.CompaniesLoadedMsg{Err: loadErr}, activeApplicationsLoadedMsg{}},
		{"companies fail, postings succeed", message.CompaniesLoadedMsg{Err: loadErr}, postingsLoadedMsg{}},
		{"companies fail, application succeeds", message.CompaniesLoadedMsg{Err: loadErr}, applicationLoadedMsg{}},
		{"companies fail, document reviews succeed", message.CompaniesLoadedMsg{Err: loadErr}, documentReviewsLoadedMsg{}},
		{"companies fail, posting reload succeeds", message.CompaniesLoadedMsg{Err: loadErr}, postingReloadedMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := newTestApp(t, testutil.NewTestApp(t), nil)
			app = sendKeyAndApply(t, app, tt.failed)
			app, _ = sendKey(app, tt.succeed)
			if !strings.Contains(app.banner(), loadErr.Error()) {
				t.Errorf("banner = %q, want it to show %q", app.banner(), loadErr)
			}
		})
	}
}

func TestApp_ApplicationDetailReloadSucceedsAfterAnotherLoadFails_KeepsTheError(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	application := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after enter = %v, want application detail", app.currentScreen)
	}
	loadErr := errors.New("disk on fire")

	app = sendKeyAndApply(t, app, message.CompaniesLoadedMsg{Err: loadErr})
	app, _ = sendKey(app, applicationDetailReloadedMsg{id: application.ID, application: application})

	if !strings.Contains(app.banner(), loadErr.Error()) {
		t.Errorf("banner = %q, want it to show %q", app.banner(), loadErr)
	}
}

func TestApp_PostingDetail_StaleReview_ShowsNotReviewedNotOldFlag(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)

	posting := app.postings[0]
	application, err := createApplicationIn(context.Background(), s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("original draft"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}
	if _, err := createReview(context.Background(), s, application.ID, document.CoverLetter, "original draft", document.Flagged, "too generic"); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	app = openPostingDetail(t, app)
	if !strings.Contains(viewText(app), "[FLAGGED]") {
		t.Fatalf("view before revision does not show [FLAGGED]:\n%s", viewText(app))
	}

	// Revise the document (e.g. the agent/user addressing the feedback)
	// without submitting a new review.
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("revised draft addressing the feedback"), 0o644); err != nil {
		t.Fatalf("rewrite cover letter: %v", err)
	}
	// Leave and re-enter to trigger a fresh load (mirrors how a real
	// session would pick up the on-disk change).
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	app = openPostingDetail(t, app)

	view := viewText(app)
	if strings.Contains(view, "[FLAGGED]") {
		t.Errorf("view after revision still shows [FLAGGED] -- stale review should be treated as not reviewed:\n%s", view)
	}
	if strings.Contains(view, "too generic") {
		t.Errorf("view after revision still shows the stale review's notes:\n%s", view)
	}
	if !strings.Contains(view, "[not reviewed]") {
		t.Errorf("view after revision does not show [not reviewed] for the cover letter:\n%s", view)
	}
}

// TestApp_PostingDetail_PressU_RefreshesWithoutLeavingScreen covers the
// case an external agent revises a document on disk (e.g. the
// apply-to-posting skill addressing a flagged review) while the user is
// still sitting on posting detail: unlike
// TestApp_PostingDetail_StaleReview_ShowsNotReviewedNotOldFlag, which
// picks up the change by leaving and re-entering, this exercises the
// manual refresh key ('u') that reloads in place.
func TestApp_PostingDetail_PressU_RefreshesWithoutLeavingScreen(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)

	posting := app.postings[0]
	application, err := createApplicationIn(context.Background(), s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("original draft"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}
	if _, err := createReview(context.Background(), s, application.ID, document.CoverLetter, "original draft", document.Flagged, "too generic"); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	app = openPostingDetail(t, app)
	if !strings.Contains(viewText(app), "[FLAGGED]") {
		t.Fatalf("view before revision does not show [FLAGGED]:\n%s", viewText(app))
	}

	// Revise the document in place, without leaving posting detail --
	// simulates the agent rewriting the file while the TUI stays open.
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("revised draft addressing the feedback"), 0o644); err != nil {
		t.Fatalf("rewrite cover letter: %v", err)
	}

	app, cmd := sendKey(app, runeKey('u'))
	if cmd == nil {
		t.Fatal("Update on 'u' returned nil Cmd, want a command that refreshes posting detail")
	}
	app = applyCmd(t, app, cmd)

	view := viewText(app)
	if strings.Contains(view, "[FLAGGED]") {
		t.Errorf("view after 'u' still shows [FLAGGED] -- refresh should have picked up the revision:\n%s", view)
	}
	if !strings.Contains(view, "[not reviewed]") {
		t.Errorf("view after 'u' does not show [not reviewed] for the revised cover letter:\n%s", view)
	}
}

func TestApp_SubmitDocumentReview_ShowsImmediatelyOnPostingDetail(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)

	posting := app.postings[0]
	application, err := createApplicationIn(context.Background(), s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("# Cover Letter"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	if app.currentScreen != screenDocumentReviewSelect {
		t.Fatalf("screen after 'r' = %v, want screenDocumentReviewSelect", app.currentScreen)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // select "Cover Letter" (cursor 0)
	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after selecting cover letter = %v, want screenDocumentReviewForm", app.currentScreen)
	}
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // pass
	if cmd == nil {
		t.Fatal("Update on ctrl+s returned nil Cmd, want a command that saves the review")
	}
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after saving review = %v, want screenPostingDetail", app.currentScreen)
	}
	view := viewText(app)
	if !strings.Contains(view, "[PASSED]") {
		t.Errorf("view after submitting review does not show [PASSED] without navigating away and back:\n%s", view)
	}
}

func TestApp_ActiveApplications_Enter_EntersApplicationDetail(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	if app.currentScreen != screenActiveApplications {
		t.Fatalf("initial screen = %v, want screenActiveApplications", app.currentScreen)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after enter = %v, want screenApplicationDetail", app.currentScreen)
	}
	if app.applicationDetail.application.Posting.Title != "Engineer" {
		t.Fatalf("applicationDetail.application.Posting.Title = %q, want %q", app.applicationDetail.application.Posting.Title, "Engineer")
	}
}

func TestApp_ApplicationDetail_P_EntersPostingDetail(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
	// No Cmd expected: applicationDetail's posting.ActiveApplication already has the
	// full posting/application/reviews loaded (from active-applications),
	// so entering posting detail from here renders synchronously rather
	// than round-tripping through a.postings-dependent lookupPosting (see
	// #86 follow-up).
	app, _ = sendKey(app, runeKey('p'))

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after 'p' = %v, want screenPostingDetail", app.currentScreen)
	}
	if app.postingDetail.posting.ID != posting.ID {
		t.Fatalf("postingDetail.posting.ID = %d, want %d", app.postingDetail.posting.ID, posting.ID)
	}
}

// TestApp_PostingDetailFromApplicationDetail_Esc_ReturnsToApplicationDetail
// is a regression test: backToPostingListMsg's handler used to
// unconditionally set screen = screenPostingList, which is wrong when
// posting detail was reached from application detail rather than by
// browsing a company's postings -- a.postings is empty in that case (it's
// only populated by screenPostingList's own loadPostings), so esc landed
// on an empty, disconnected posting-list screen instead of back where the
// user came from. returnStack now tracks where posting detail was entered
// from, the same way it does for the other multi-entry screens (see
// #86 follow-up and #89).
func TestApp_PostingDetailFromApplicationDetail_Esc_ReturnsToApplicationDetail(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
	app, _ = sendKey(app, runeKey('p'))                        // application detail -> posting detail
	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after 'p' = %v, want screenPostingDetail", app.currentScreen)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after esc = %v, want screenApplicationDetail (not screenPostingList, which was never browsed)", app.currentScreen)
	}
}

// TestApp_StatusChangeFromApplicationDetailFastPath_PreservesPostingFields
// is a regression test for a bug caught in code review: applicationDetail's
// 'p' enters posting detail without ever populating a.postings (the fast
// path -- #87 follow-up). applicationStatusUpdatedMsg's
// handler used to unconditionally rebuild a.postingDetail via
// lookupPosting(a.postingDetail.posting.ID), which searches a.postings and
// returns a zero-value posting.Posting{} when not found -- so changing status
// from a posting-detail screen reached this way silently blanked the
// displayed posting's title/department/location/etc, even though the
// status write itself succeeded. rebuildPostingDetailApplication fixes this
// by keeping the already-displayed posting instead of re-deriving it.
func TestApp_StatusChangeFromApplicationDetailFastPath_PreservesPostingFields(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
	app, _ = sendKey(app, runeKey('p'))                        // application detail -> posting detail (fast path)
	if app.postingDetail.posting.Title != "Engineer" {
		t.Fatalf("postingDetail.posting.Title before status change = %q, want %q", app.postingDetail.posting.Title, "Engineer")
	}

	app = sendKeyAndApply(t, app, runeKey('s'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter (status select) returned nil Cmd, want a command that updates the status")
	}
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after status change = %v, want screenPostingDetail", app.currentScreen)
	}
	if app.postingDetail.posting.Title != "Engineer" {
		t.Fatalf("postingDetail.posting.Title after status change = %q, want %q (lookupPosting must not clobber the posting when a.postings was never populated)", app.postingDetail.posting.Title, "Engineer")
	}
}

func TestApp_ApplicationDetail_ShiftR_EntersReviewFormDirectly(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.Resume).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.Resume).Path, []byte("# Resume"), 0o644); err != nil {
		t.Fatalf("WriteFile resume: %v", err)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
	app, _ = sendKey(app, runeKey('R'))
	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after 'R' = %v, want screenDocumentReviewForm (no picker step needed -- this screen already knows which document)", app.currentScreen)
	}
}

func TestApp_ApplicationDetail_CancelDocumentReview_ReturnsToApplicationDetail(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.Resume).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.Resume).Path, []byte("# Resume"), 0o644); err != nil {
		t.Fatalf("WriteFile resume: %v", err)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, _ = sendKey(app, runeKey('R'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after cancelling review = %v, want screenApplicationDetail (not screenActiveApplications)", app.currentScreen)
	}
}

// TestApp_SubmitDocumentReviewFromApplicationDetail_UpdatesBadgeImmediately
// mirrors TestApp_SubmitDocumentReview_ShowsImmediatelyOnPostingDetail
// above, but entering review from application detail (reached via the
// active-applications "application view" -- #83 and
// its #86 follow-up reorganizing this workflow).
func TestApp_SubmitDocumentReviewFromApplicationDetail_UpdatesBadgeImmediately(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.Resume).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.Resume).Path, []byte("# Resume"), 0o644); err != nil {
		t.Fatalf("WriteFile resume: %v", err)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, _ = sendKey(app, runeKey('R'))
	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after 'R' = %v, want screenDocumentReviewForm", app.currentScreen)
	}
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // pass
	if cmd == nil {
		t.Fatal("Update on ctrl+s returned nil Cmd, want a command that saves the review")
	}
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after saving review = %v, want screenApplicationDetail", app.currentScreen)
	}
	view := viewText(app)
	if !strings.Contains(view, "[PASSED]") {
		t.Errorf("view after submitting review does not show [PASSED] without navigating away and back:\n%s", view)
	}
}

// TestApp_SubmitDocumentReviewFromApplicationDetail_UpdatesActiveApplicationsGlyph
// covers the gap noted in the #92 follow-up to PR #88's code review:
// application detail's own badge was already covered (see
// TestApp_SubmitDocumentReviewFromApplicationDetail_UpdatesBadgeImmediately
// above), but nothing asserted that backing out to
// screenActiveApplications afterward shows the compact glyph column
// (reviewGlyphSummary, e.g. "R:✓") reflecting the same change --
// which documentReviewsLoadedMsg now patches into that application's
// row, with no test to catch a regression if that were ever
// dropped.
func TestApp_SubmitDocumentReviewFromApplicationDetail_UpdatesActiveApplicationsGlyph(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.Resume).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.Resume).Path, []byte("# Resume"), 0o644); err != nil {
		t.Fatalf("WriteFile resume: %v", err)
	}

	if strings.Contains(viewText(app), "R:✓") {
		t.Fatal("active-applications view already shows R:✓ before any review was submitted")
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, _ = sendKey(app, runeKey('R'))
	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after 'R' = %v, want screenDocumentReviewForm", app.currentScreen)
	}
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // pass
	if cmd == nil {
		t.Fatal("Update on ctrl+s returned nil Cmd, want a command that saves the review")
	}
	app = applyCmd(t, app, cmd)

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.currentScreen != screenActiveApplications {
		t.Fatalf("screen after esc = %v, want screenActiveApplications", app.currentScreen)
	}
	view := viewText(app)
	if !strings.Contains(view, "R:✓") {
		t.Errorf("active-applications view after esc does not show R:✓ for the just-passed resume review:\n%s", view)
	}
}

// TestApp_SubmitDocumentReviewFromApplicationDetail_PatchesActiveApplicationRow
// covers #91: the reviewed application's row on the home screen is
// patched from the reviews application detail already reloads, rather
// than by reloading every active application (which queries this
// application's reviews a second time). The row's Next column depends on
// those reviews too, so it has to move on with the glyph: with the cover
// letter already passed, passing the resume turns "review" into "export".
func TestApp_SubmitDocumentReviewFromApplicationDetail_PatchesActiveApplicationRow(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	if _, err := createReview(context.Background(), s, application.ID, document.CoverLetter, "# Cover Letter", document.Passed, ""); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.Resume).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for documentType, content := range map[document.Type]string{document.CoverLetter: "# Cover Letter", document.Resume: "# Resume"} {
		if err := os.WriteFile(mustDoc(t, status, documentType).Path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", documentType, err)
		}
	}
	app = applyCmd(t, app, app.Init())
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	if !strings.Contains(viewText(app), "review") {
		t.Fatalf("active-applications view before the resume review doesn't show next step \"review\":\n%s", viewText(app))
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, _ = sendKey(app, runeKey('R'))
	app, save := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // pass
	if save == nil {
		t.Fatal("Update on ctrl+s returned nil Cmd, want a command that saves the review")
	}
	app, reload := sendKey(app, save())
	for _, msg := range runCmd(t, reload) {
		if _, isFullReload := msg.(activeApplicationsLoadedMsg); isFullReload {
			t.Error("submitting a review from application detail reloaded every active application, want only this application's reviews")
		}
		app, _ = sendKey(app, msg)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.currentScreen != screenActiveApplications {
		t.Fatalf("screen after esc = %v, want screenActiveApplications", app.currentScreen)
	}
	view := viewText(app)
	if !strings.Contains(view, "R:✓") {
		t.Errorf("active-applications view after esc does not show R:✓ for the just-passed resume review:\n%s", view)
	}
	if !strings.Contains(view, "export") {
		t.Errorf("active-applications view after esc does not show next step \"export\":\n%s", view)
	}
}

// runCmd runs cmd and returns the messages it produces, flattening any
// tea.BatchMsg, without applying them -- for a test that inspects which
// messages a Cmd sends.
func runCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, runCmd(t, c)...)
	}
	return msgs
}

// TestApp_SubmitDocumentReviewFromPostingDetailViaApplicationDetailFastPath_KeepsApplication
// is a regression test: application detail's 'p' enters posting detail via
// the fast path (#87 follow-up), which never populates
// a.applicationsByPosting since it renders directly from the already-loaded
// posting.ActiveApplication instead of going through loadApplication. Submitting a
// document review from that posting-detail screen returns to
// screenPostingDetail, and
// documentReviewCreatedMsg's handler for that case calls
// rebuildPostingDetailApplication, which re-derives hasApp via
// lookupPosting -- a miss against the never-populated map -- and wrongly
// renders "No application started" even though the application is real and
// unchanged.
func TestApp_SubmitDocumentReviewFromPostingDetailViaApplicationDetailFastPath_KeepsApplication(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("# Cover Letter"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
	app, _ = sendKey(app, runeKey('p'))                        // application detail -> posting detail (fast path)
	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after 'p' = %v, want screenPostingDetail", app.currentScreen)
	}

	app, _ = sendKey(app, runeKey('r')) // posting detail -> document review select
	if app.currentScreen != screenDocumentReviewSelect {
		t.Fatalf("screen after 'r' = %v, want screenDocumentReviewSelect", app.currentScreen)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // select "Cover Letter" (cursor 0)
	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after selecting cover letter = %v, want screenDocumentReviewForm", app.currentScreen)
	}
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // pass
	if cmd == nil {
		t.Fatal("Update on ctrl+s returned nil Cmd, want a command that saves the review")
	}
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after saving review = %v, want screenPostingDetail", app.currentScreen)
	}
	if !app.postingDetail.hasApplication {
		t.Fatal("postingDetail.hasApplication = false after submitting a review via the application-detail fast path, want true (application was never removed)")
	}
	view := viewText(app)
	if strings.Contains(view, "No application started") {
		t.Errorf("view after submitting review shows \"No application started\" even though the application is real and unchanged:\n%s", view)
	}
	if !strings.Contains(view, "[PASSED]") {
		t.Errorf("view after submitting review does not show [PASSED]:\n%s", view)
	}
}

// TestApp_ApplicationDetail_PressU_RefreshesWithoutLeavingScreen mirrors
// TestApp_PostingDetail_PressU_RefreshesWithoutLeavingScreen for
// application detail: an external agent revising a document on disk
// while the user is looking at this screen should be picked up by
// pressing 'u', without having to back out and re-enter.
func TestApp_ApplicationDetail_PressU_RefreshesWithoutLeavingScreen(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	docs := documents.NewStore(t.TempDir())
	status := docs.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("original draft"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}
	if _, err := createReview(context.Background(), s, application.ID, document.CoverLetter, "original draft", document.Flagged, "too generic"); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	// Constructed (and Init-loaded) only after the file/review already
	// exist, so the initial active-applications load picks them up --
	// mirrors a fresh TUI session started after the review already
	// happened, distinct from the in-place refresh this test exercises.
	app := newAppOn(s, newTestSyncer(s, nil), docs)
	app = applyCmd(t, app, app.Init())
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after enter = %v, want screenApplicationDetail", app.currentScreen)
	}
	if !strings.Contains(viewText(app), "[FLAGGED]") {
		t.Fatalf("view before revision does not show [FLAGGED]:\n%s", viewText(app))
	}

	// Revise the document in place, without leaving application detail.
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("revised draft addressing the feedback"), 0o644); err != nil {
		t.Fatalf("rewrite cover letter: %v", err)
	}

	app, cmd := sendKey(app, runeKey('u'))
	if cmd == nil {
		t.Fatal("Update on 'u' returned nil Cmd, want a command that refreshes application detail")
	}
	app = applyCmd(t, app, cmd)

	view := viewText(app)
	if strings.Contains(view, "[FLAGGED]") {
		t.Errorf("view after 'u' still shows [FLAGGED] -- refresh should have picked up the revision:\n%s", view)
	}
	if !strings.Contains(view, "[not reviewed]") {
		t.Errorf("view after 'u' does not show [not reviewed] for the revised cover letter:\n%s", view)
	}
}

func TestApp_PostingDetail_ApplicationExistsNoFiles_ShowsNotFoundStatus(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)

	posting := app.postings[0]
	application, err := createApplicationIn(context.Background(), s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	// Deliberately no files written -- the application row exists, but
	// the documents don't yet.
	status := app.documents.Status(application.ID)

	app = openPostingDetail(t, app)

	view := viewText(app)
	if !strings.Contains(view, "not found ("+mustDoc(t, status, document.CoverLetter).Path+")") {
		t.Errorf("view does not show cover letter not found with path %q:\n%s", mustDoc(t, status, document.CoverLetter).Path, view)
	}
	if !strings.Contains(view, "not found ("+mustDoc(t, status, document.Resume).Path+")") {
		t.Errorf("view does not show resume not found with path %q:\n%s", mustDoc(t, status, document.Resume).Path, view)
	}
}

func TestApp_PostingDetail_NoApplication_ShowsNoDocumentsSection(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)
	// No application.Create call -- application.Get will return
	// store.ErrNotFound, per the "no application -> show nothing"
	// decision.

	app = openPostingDetail(t, app)

	view := viewText(app)
	if strings.Contains(view, "Documents") {
		t.Errorf("view shows a Documents section with no Application row:\n%s", view)
	}
	if strings.Contains(view, "Cover Letter") || strings.Contains(view, "Resume") {
		t.Errorf("view mentions cover letter/resume with no Application row:\n%s", view)
	}
}

func TestApp_CompanyList_VimJK_MoveCursorLikeArrows(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendKey(app, runeKey('j'))
	if app.companyList.cursor != 1 {
		t.Fatalf("cursor after 'j' = %d, want 1", app.companyList.cursor)
	}
	app, _ = sendKey(app, runeKey('k'))
	if app.companyList.cursor != 0 {
		t.Fatalf("cursor after 'k' = %d, want 0", app.companyList.cursor)
	}
}

func TestApp_PostingList_VimJK_MoveCursorLikeArrows(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app, _ = sendKey(app, runeKey('j'))
	if app.postingList.cursor != 1 {
		t.Fatalf("postingCursor after 'j' = %d, want 1", app.postingList.cursor)
	}
	app, _ = sendKey(app, runeKey('k'))
	if app.postingList.cursor != 0 {
		t.Fatalf("postingCursor after 'k' = %d, want 0", app.postingList.cursor)
	}
}

func TestApp_PostingDetail_VimHL_MovesBetweenPostingsLikeArrows(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	// ListPostingsByCompany orders first_seen_at DESC, id DESC, so the
	// later-inserted job-2/Designer sorts first -- detail starts on
	// Designer.
	if app.postingDetail.posting.Title != "Designer" {
		t.Fatalf("initial posting shown in detail = %q, want %q", app.postingDetail.posting.Title, "Designer")
	}

	app, _ = sendKey(app, runeKey('l'))
	if app.postingDetail.posting.Title != "Engineer" {
		t.Fatalf("posting shown after 'l' = %q, want %q", app.postingDetail.posting.Title, "Engineer")
	}
	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after 'l' = %v, want screenPostingDetail (stay in detail)", app.currentScreen)
	}

	app, _ = sendKey(app, runeKey('h'))
	if app.postingDetail.posting.Title != "Designer" {
		t.Fatalf("posting shown after 'h' = %q, want %q", app.postingDetail.posting.Title, "Designer")
	}
}

func TestApp_PostingDetail_LongLine_WrapsToViewportWidth(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	longLine := strings.Repeat("word ", 40)
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longLine}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 20, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	for _, line := range strings.Split(app.postingDetail.viewport.View(), "\n") {
		if w := lipgloss.Width(line); w > 20 {
			t.Fatalf("rendered line %q has width %d, want <= 20", line, w)
		}
	}
}

func TestApp_WindowResize_OnPostingDetail_ReWraps(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	// A distinctive marker at the end: if resize truncates instead of
	// re-wrapping, this gets silently cut off rather than pushed onto a
	// new line -- catches truncation that a mere "no line exceeds width"
	// check would miss (viewport.View() uses lipgloss MaxWidth, which
	// truncates already-wrapped content instead of re-wrapping it).
	longLine := strings.Repeat("word ", 40) + "LASTWORD"
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longLine}},
	})
	app := newTestApp(t, s, syncer)
	// Tall viewport (200 rows) so every wrapped line is visible in View()
	// at once -- isolates the width re-wrap behavior from scroll/height.
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 200})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 15, Height: 200})

	view := app.postingDetail.viewport.View()
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > 15 {
			t.Fatalf("rendered line %q has width %d, want <= 15 after resize", line, w)
		}
	}
	if !strings.Contains(view, "LASTWORD") {
		t.Fatal("LASTWORD missing after resize -- content was truncated, not re-wrapped")
	}
}

func TestApp_WindowSizeMsg_UpdatesDimensions(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 24})

	if app.width != 80 || app.height != 24 {
		t.Fatalf("app.width, app.height = %d, %d, want 80, 24", app.width, app.height)
	}
}

func TestApp_PressO_OnPostingDetail_OpensJobURLInBrowser(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", JobURL: "https://jobs.ashbyhq.com/acme/job-1"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)

	_, cmd := sendKey(app, runeKey('o'))
	if cmd == nil {
		t.Fatal("Update on 'o' returned nil Cmd, want a command that opens the browser")
	}
}

func TestApp_PressO_OnPostingList_OpensSelectedPostingJobURLInBrowser(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", JobURL: "https://jobs.ashbyhq.com/acme/job-1"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	_, cmd := sendKey(app, runeKey('o'))
	if cmd == nil {
		t.Fatal("Update on 'o' returned nil Cmd, want a command that opens the browser")
	}
}

func TestApp_PressF_OnPostingList_OpensFilterSelectAndLoadsOptions(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app, cmd := sendKey(app, runeKey('f'))
	if cmd == nil {
		t.Fatal("Update on 'f' returned nil Cmd, want a command that loads filter options")
	}
	if app.currentScreen != screenFilterSelect {
		t.Fatalf("screen after 'f' = %v, want screenFilterSelect", app.currentScreen)
	}

	app, _ = sendKey(app, cmd())

	if diff := cmp.Diff([]string{"Engineering"}, app.filterSelect.departmentOptions); diff != "" {
		t.Fatalf("filterDepartmentOptions mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Remote"}, app.filterSelect.locationOptions); diff != "" {
		t.Fatalf("filterLocationOptions mismatch (-want +got):\n%s", diff)
	}
}

func TestApp_FilterSelect_SpaceTogglesSelection(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app, cmd := sendKey(app, runeKey('f'))
	app, _ = sendKey(app, cmd())

	if app.filterSelect.selectedDepartments["Engineering"] {
		t.Fatal("Engineering should not be selected initially")
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !app.filterSelect.selectedDepartments["Engineering"] {
		t.Fatal("Engineering should be selected after space")
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if app.filterSelect.selectedDepartments["Engineering"] {
		t.Fatal("Engineering should be deselected after second space")
	}
}

func TestApp_FilterSelect_Esc_CancelsWithoutSaving(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app, cmd := sendKey(app, runeKey('f'))
	app, _ = sendKey(app, cmd())

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.currentScreen != screenPostingList {
		t.Fatalf("screen after esc = %v, want screenPostingList", app.currentScreen)
	}

	saved, err := s.Companies.Filters(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("companyfilter.List: %v", err)
	}
	if len(saved) != 0 {
		t.Fatalf("saved filters = %+v, want none (esc should not persist selection)", saved)
	}
}

func TestApp_FilterSelect_Enter_SavesAndNarrows(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"},
			{SourceID: "job-2", Title: "Salesperson", Department: "Sales", Location: "Remote"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	if len(app.postings) != 2 {
		t.Fatalf("initial postings = %+v, want 2", app.postings)
	}

	app, cmd := sendKey(app, runeKey('f'))
	app, _ = sendKey(app, cmd())

	// Cursor starts at filterDepartmentOptions[0]. Confirm which
	// department that is before toggling, so the assertion below isn't
	// order-dependent.
	wantDept := app.filterSelect.departmentOptions[0]
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})

	// Enter narrows synchronously (no round trip needed) and returns the
	// saveCompanyFilters Cmd that persists the selection.
	app, cmd = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenPostingList {
		t.Fatalf("screen after enter = %v, want screenPostingList", app.currentScreen)
	}
	if len(app.postings) != 1 {
		t.Fatalf("postings after enter = %+v, want 1 (narrowed to %s, before saveCompanyFilters' Cmd even runs)", app.postings, wantDept)
	}
	if app.postings[0].Department != wantDept {
		t.Fatalf("remaining posting department = %q, want %q", app.postings[0].Department, wantDept)
	}
	if cmd == nil {
		t.Fatal("Update on enter returned nil Cmd, want a command that applies the filters")
	}

	app, cmd = sendKey(app, cmd())

	saved, err := s.Companies.Filters(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("companyfilter.List: %v", err)
	}
	if len(saved) != 1 || saved[0].Field != filter.FieldDepartment || saved[0].Value != wantDept {
		t.Fatalf("saved filters = %+v, want one department=%s filter", saved, wantDept)
	}
	if want := "Acme filters saved"; app.status != want {
		t.Errorf("app.status = %q, want %q", app.status, want)
	}
	if cmd == nil {
		t.Fatal("Update on companyFiltersSavedMsg returned nil Cmd, want a command that reloads postings")
	}

	// Drive the reload: this must NOT silently undo the narrowing by
	// loading everything unfiltered -- ListPostingsByCompany itself has
	// no notion of company_filters, so the reload has to re-apply them,
	// not just re-fetch.
	app, _ = sendKey(app, cmd())

	if len(app.postings) != 1 {
		t.Fatalf("postings after post-save reload = %+v, want still 1 (filter should survive the reload)", app.postings)
	}
	if app.postings[0].Department != wantDept {
		t.Fatalf("posting department after reload = %q, want %q", app.postings[0].Department, wantDept)
	}
}

// TestApp_FilterSelect_Enter_WhileCompanySyncsElsewhere_Saves: saving
// filters doesn't sync, so another sync holding the company's lease
// changes nothing about it.
func TestApp_FilterSelect_Enter_WhileCompanySyncsElsewhere_Saves(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"},
			{SourceID: "job-2", Title: "Salesperson", Department: "Sales", Location: "Remote"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	if taken, err := s.Companies.AcquireSyncLease(context.Background(), acme.ID, "swamp-fetch", time.Minute); err != nil || !taken {
		t.Fatalf("AcquireSyncLease = %v, %v", taken, err)
	}

	app, cmd := sendKey(app, runeKey('f'))
	app, _ = sendKey(app, cmd())
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	app, cmd = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, cmd = sendKey(app, cmd())

	saved, err := s.Companies.Filters(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("companyfilter.List: %v", err)
	}
	if len(saved) != 1 {
		t.Errorf("saved filters = %+v, want the selection saved", saved)
	}
	if app.err != nil {
		t.Errorf("app.err = %v, want nil", app.err)
	}
	if want := "Acme filters saved"; app.status != want {
		t.Errorf("app.status = %q, want %q", app.status, want)
	}
	if cmd == nil {
		t.Error("Update on the save returned nil Cmd, want a command that reloads postings")
	}
}

func TestApp_CompanyRefreshed_ForCurrentlyViewedCompany_ReloadsPostings(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	// Simulate a background re-sync (r on the posting list)
	// completing for the company currently being viewed.
	app, cmd := sendKey(app, message.CompanyRefreshedMsg{
		Result: sync.Result{CompanyID: acme.ID, Name: acme.Name},
	})
	if cmd == nil {
		t.Fatal("Update on message.CompanyRefreshedMsg for the viewed company returned nil Cmd, want a command that reloads postings")
	}

	app, _ = sendKey(app, cmd())
	if len(app.postings) != 1 {
		t.Fatalf("postings after reload = %+v, want 1", app.postings)
	}
}

func TestApp_CompanyRefreshed_ForDifferentCompany_DoesNotReloadPostings(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	_, cmd := sendKey(app, message.CompanyRefreshedMsg{
		Result: sync.Result{CompanyID: 999999, Name: "SomeOtherCompany"},
	})
	// The company list still reloads (its Open and Last fetched columns
	// changed), but the postings being viewed belong to a different company
	// and must be left alone.
	if cmd == nil {
		t.Fatal("Update on message.CompanyRefreshedMsg returned nil Cmd, want one that reloads companies")
	}
	msgs := []tea.Msg{cmd()}
	if batch, ok := msgs[0].(tea.BatchMsg); ok {
		msgs = nil
		for _, c := range batch {
			msgs = append(msgs, c())
		}
	}
	reloadsCompanies := false
	for _, msg := range msgs {
		if _, ok := msg.(postingsLoadedMsg); ok {
			t.Fatal("refresh of a different company reloaded the postings being viewed")
		}
		reloadsCompanies = reloadsCompanies || isCompaniesLoaded(msg)
	}
	if !reloadsCompanies {
		t.Fatalf("refresh of a different company produced %#v, want a message.CompaniesLoadedMsg among them", msgs)
	}
}

// A refresh refused because another process is syncing the company has no
// result, so the status names the company the refresh was started for.
func TestApp_CompanyRefreshed_SyncInProgress_SaysWhichCompany(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app = sendKeyAndApply(t, app, message.CompanyRefreshedMsg{Name: "Acme", Err: sync.ErrSyncInProgress})

	if app.err != nil {
		t.Errorf("app.err = %v, want nil: a sync elsewhere isn't a failure", app.err)
	}
	if want := "Acme is already syncing elsewhere"; !strings.Contains(app.status, want) {
		t.Errorf("app.status = %q, want it to contain %q", app.status, want)
	}
}

func TestApp_OpenPostingList_WithPreExistingSavedFilters_NarrowsOnFirstLoad(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	addCompanyFilter(t, s, acme.ID, filter.FieldDepartment, "Engineering")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer", Department: "Engineering"},
			{SourceID: "job-2", Title: "Salesperson", Department: "Sales"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	if len(app.postings) != 1 {
		t.Fatalf("postings on first load = %+v, want 1 (narrowed by pre-existing saved filter, simulating a prior session)", app.postings)
	}
	if app.postings[0].Department != "Engineering" {
		t.Fatalf("posting department = %q, want %q", app.postings[0].Department, "Engineering")
	}
}

func TestApp_OpenPostingList_TracksActiveFiltersFromExisting(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	addCompanyFilter(t, s, acme.ID, filter.FieldDepartment, "Engineering")
	addCompanyFilter(t, s, acme.ID, filter.FieldLocation, "Remote")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	if diff := cmp.Diff([]string{"Engineering"}, app.activeFilterDepartments); diff != "" {
		t.Fatalf("activeFilterDepartments mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Remote"}, app.activeFilterLocations); diff != "" {
		t.Fatalf("activeFilterLocations mismatch (-want +got):\n%s", diff)
	}
}

func TestApp_FilterSelect_Enter_UpdatesActiveFiltersImmediately(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	if len(app.activeFilterDepartments) != 0 {
		t.Fatalf("initial activeFilterDepartments = %+v, want empty", app.activeFilterDepartments)
	}

	app, cmd := sendKey(app, runeKey('f'))
	app, _ = sendKey(app, cmd())
	wantDept := app.filterSelect.departmentOptions[0]
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	// Updated synchronously on enter -- no need to run the returned
	// saveCompanyFilters Cmd to observe it.
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})

	if diff := cmp.Diff([]string{wantDept}, app.activeFilterDepartments); diff != "" {
		t.Fatalf("activeFilterDepartments after enter mismatch (-want +got):\n%s", diff)
	}
}

func TestFilterSummaryLine_NoFilters_ReturnsEmpty(t *testing.T) {
	if got := filterSummaryLine(nil, nil); got != "" {
		t.Fatalf("filterSummaryLine(nil, nil) = %q, want empty", got)
	}
}

func TestFilterSummaryLine_DepartmentsOnly(t *testing.T) {
	got := filterSummaryLine([]string{"Engineering", "Sales"}, nil)
	want := "Filtering: Department: Engineering, Sales"
	if got != want {
		t.Fatalf("filterSummaryLine = %q, want %q", got, want)
	}
}

func TestFilterSummaryLine_DepartmentsAndLocations(t *testing.T) {
	got := filterSummaryLine([]string{"Engineering"}, []string{"Remote"})
	want := "Filtering: Department: Engineering | Location: Remote"
	if got != want {
		t.Fatalf("filterSummaryLine = %q, want %q", got, want)
	}
}

func TestFilterPostingsByCompanyFilters_MatchSemantics(t *testing.T) {
	t.Parallel()

	eng := posting.Posting{ID: 1, IngestedFields: posting.IngestedFields{Department: "Engineering", Location: "Remote"}}
	sales := posting.Posting{ID: 2, IngestedFields: posting.IngestedFields{Department: "Sales", Location: "Remote"}}
	engNYC := posting.Posting{ID: 3, IngestedFields: posting.IngestedFields{Department: "Engineering", Location: "New York"}}
	postings := []posting.Posting{eng, sales, engNYC}

	tests := []struct {
		name    string
		filters []filter.Filter
		want    []posting.Posting
	}{
		{
			name:    "no filters matches everything",
			filters: nil,
			want:    postings,
		},
		{
			name:    "department filter alone",
			filters: []filter.Filter{{Field: filter.FieldDepartment, Value: "Engineering"}},
			want:    []posting.Posting{eng, engNYC},
		},
		{
			name:    "location filter alone",
			filters: []filter.Filter{{Field: filter.FieldLocation, Value: "Remote"}},
			want:    []posting.Posting{eng, sales},
		},
		{
			name: "department and location AND together, OR within a field",
			filters: []filter.Filter{
				{Field: filter.FieldDepartment, Value: "Engineering"},
				{Field: filter.FieldDepartment, Value: "Sales"},
				{Field: filter.FieldLocation, Value: "Remote"},
			},
			want: []posting.Posting{eng, sales},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := filterPostingsByCompanyFilters(postings, tt.filters)
			if err != nil {
				t.Fatalf("filterPostingsByCompanyFilters: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("filterPostingsByCompanyFilters mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// splitCompanyFilters ignores fields it doesn't know, so without this an
// unsupported field would be silently not applied rather than an error.
func TestFilterPostingsByCompanyFilters_UnsupportedField_ReturnsError(t *testing.T) {
	t.Parallel()

	postings := []posting.Posting{{ID: 1, IngestedFields: posting.IngestedFields{Department: "Engineering"}}}
	filters := []filter.Filter{{Field: "not_a_real_field", Value: "x"}}

	_, err := filterPostingsByCompanyFilters(postings, filters)
	if err == nil {
		t.Fatal("filterPostingsByCompanyFilters err = nil, want an error for an unsupported field")
	}
}

// TestApp_PostingsLoadedMsgWithErr_ClearsPostingsAndMarkup pins the
// App-level consequence of loadPostings returning an error (e.g. from
// filterPostingsByCompanyFilters, though company_filters' CHECK
// constraint makes that specific trigger unreachable in practice --
// postingsLoadedMsg{err:...} can come from any of loadPostings' several
// fallible steps): the Update handler unconditionally applies
// msg.postings/msg.markup/msg.departments/msg.locations, so an error
// blanks the previously-loaded list rather than leaving it visible
// alongside the error. Documented as a deliberate trade-off (see
// #61) rather than something this test argues should
// change -- it exists so a future change to this behavior is a visible,
// intentional diff instead of a silent one.
func TestApp_PostingsLoadedMsgWithErr_ClearsPostingsAndMarkup(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	if len(app.postings) != 1 {
		t.Fatalf("postings before error = %+v, want 1", app.postings)
	}

	app, _ = sendKey(app, postingsLoadedMsg{err: errors.New("boom")})

	if app.postings != nil {
		t.Fatalf("postings after error = %+v, want nil (cleared)", app.postings)
	}
	if app.postingMarkup != nil {
		t.Fatalf("postingMarkup after error = %+v, want nil (cleared)", app.postingMarkup)
	}
	if app.err == nil {
		t.Fatal("app.err = nil, want the propagated error")
	}
}

// exportTestApp builds an app sitting on the active-applications screen
// with one application whose resume is drafted on disk -- the starting
// point every export wiring test needs.
func exportTestApp(t *testing.T) *App {
	t.Helper()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.Resume).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.Resume).Path, []byte("# Resume\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile resume: %v", err)
	}
	return app
}

func TestApp_ActiveApplications_E_EntersExportScreenPrefilledWithDefaultDir(t *testing.T) {
	app := exportTestApp(t)

	app, _ = sendKey(app, runeKey('e'))

	if app.currentScreen != screenApplicationExport {
		t.Fatalf("screen after 'e' = %v, want screenApplicationExport", app.currentScreen)
	}
	if got := app.applicationExport.textinput.Value(); got != app.exportDir {
		t.Errorf("destination prefill = %q, want the app's default export dir %q", got, app.exportDir)
	}
}

func TestApp_ExportScreen_Esc_ReturnsToActiveApplications(t *testing.T) {
	app := exportTestApp(t)

	app, _ = sendKey(app, runeKey('e'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.currentScreen != screenActiveApplications {
		t.Fatalf("screen after cancelling export = %v, want screenActiveApplications", app.currentScreen)
	}
}

func TestApp_Export_ReportsWhereFilesWentAndRemembersTheDirectory(t *testing.T) {
	app := exportTestApp(t)
	dest := t.TempDir()

	app, _ = sendKey(app, runeKey('e'))
	app.applicationExport.textinput.SetValue(dest)
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenActiveApplications {
		t.Fatalf("screen after export = %v, want screenActiveApplications", app.currentScreen)
	}
	if app.err != nil {
		t.Fatalf("app.err = %v, want nil", app.err)
	}
	if !strings.Contains(app.status, dest) {
		t.Errorf("status = %q, want it to name the destination %q so the user knows where to drag from", app.status, dest)
	}
	if app.exportDir != dest {
		t.Errorf("exportDir = %q, want %q remembered for the next export", app.exportDir, dest)
	}
	exported := filepath.Join(dest, "acme-staff-engineer-resume.pdf")
	if _, err := os.Stat(exported); err != nil {
		t.Errorf("stat %s: %v", exported, err)
	}
}

func TestApp_Export_Failure_SurfacesErrorAndKeepsRememberedDir(t *testing.T) {
	app := exportTestApp(t)
	before := app.exportDir

	// A path under a regular file can't be created as a directory.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app, _ = sendKey(app, runeKey('e'))
	app.applicationExport.textinput.SetValue(filepath.Join(blocker, "sub"))
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = applyCmd(t, app, cmd)

	if app.err == nil {
		t.Fatal("app.err = nil, want the export failure surfaced")
	}
	if app.exportDir != before {
		t.Errorf("exportDir = %q, want it left at %q -- a failed export shouldn't be remembered as the new default", app.exportDir, before)
	}
}

func TestPostingDetailContent_ShowsStatusLabelNotEnumValue(t *testing.T) {
	t.Parallel()

	posting := posting.Posting{ID: 1, IngestedFields: posting.IngestedFields{Title: "Engineer"}}
	application := application.Application{ID: 1, Status: application.OfferReceived}

	got := postingDetailContent(posting, application, true, documents.NewStore(t.TempDir()), nil, 80)

	if !strings.Contains(got, "Offer received") {
		t.Errorf("postingDetailContent() = %q, want the human-readable status label", got)
	}
	if strings.Contains(got, "offer_received") {
		t.Errorf("postingDetailContent() leaks the raw enum value \"offer_received\" into the UI")
	}
}

// TestApp_PostingDetailViaApplicationDetail_HelpOmitsPostingNavigation is
// the scenario #93 reports: application detail -> 'p' -> the help line
// used to advertise "←/→ (h/l): prev/next posting" even though a.postings
// was never loaded on that path, so pressing it did nothing with no
// indication why.
func TestApp_PostingDetailViaApplicationDetail_HelpOmitsPostingNavigation(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active applications -> application detail
	app, cmd := sendKey(app, runeKey('p'))                     // fast path -> posting detail
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after 'p' = %v, want screenPostingDetail", app.currentScreen)
	}
	if got := app.postingDetail.View(); strings.Contains(got, "prev/next posting") {
		t.Errorf("posting detail reached via application detail advertises h/l, but a.postings is empty so it does nothing; view = %q", got)
	}
}

// TestApp_PostingDetailViaPostingList_HelpKeepsPostingNavigation is the
// other half: browsing a company's postings does populate a.postings, so
// the hint must still be there.
func TestApp_PostingDetailViaPostingList_HelpKeepsPostingNavigation(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})      // active applications -> company list
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // -> posting list
	app = applyCmd(t, app, cmd)
	app, cmd = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // -> posting detail
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen = %v, want screenPostingDetail", app.currentScreen)
	}
	if got := app.postingDetail.View(); !strings.Contains(got, "prev/next posting") {
		t.Errorf("posting detail reached by browsing = %q, want the h/l hint present", got)
	}
}

// TestApp_StatusSaveResolvingAfterUserLeft_DoesNotYankScreenBack covers a
// save result arriving after the user has already backed out past the
// screen that started it. Nothing blocks esc while a status save is in
// flight, so the result can land anywhere -- it must not force the screen
// back to wherever the status select was entered from.
func TestApp_StatusSaveResolvingAfterUserLeft_DoesNotYankScreenBack(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
	app, _ = sendKey(app, runeKey('p'))                        // application detail -> posting detail
	app = sendKeyAndApply(t, app, runeKey('s'))                // posting detail -> status select
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	app, saveCmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if saveCmd == nil {
		t.Fatal("Update on enter (status select) returned nil Cmd, want a command that updates the status")
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // status select -> posting detail, save still in flight
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // posting detail -> application detail
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after backing out = %v, want screenApplicationDetail", app.currentScreen)
	}

	app = applyCmd(t, app, saveCmd)

	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after late save result = %v, want screenApplicationDetail (where the user already was)", app.currentScreen)
	}
}

// #115: a save from a status select the user closed and reopened must not
// close the reopened one.
func TestApp_StatusSaveResolvingAfterReentry_KeepsReopenedScreenOpen(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
	app, _ = sendKey(app, runeKey('p'))                        // application detail -> posting detail
	app = sendKeyAndApply(t, app, runeKey('s'))                // posting detail -> status select
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	app, saveCmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if saveCmd == nil {
		t.Fatal("Update on enter (status select) returned nil Cmd, want a command that updates the status")
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // status select -> posting detail, save still in flight
	app = sendKeyAndApply(t, app, runeKey('s'))              // posting detail -> status select again
	if app.currentScreen != screenApplicationStatusSelect {
		t.Fatalf("screen after re-entering = %v, want screenApplicationStatusSelect", app.currentScreen)
	}

	app = applyCmd(t, app, saveCmd)

	if app.currentScreen != screenApplicationStatusSelect {
		t.Fatalf("screen after the earlier instance's save result = %v, want screenApplicationStatusSelect (the reopened one stays open)", app.currentScreen)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after esc from the reopened screen = %v, want screenPostingDetail", app.currentScreen)
	}
}

// TestApp_ReviewSaveResolvingAfterUserLeft_DoesNotYankScreenBack is
// TestApp_StatusSaveResolvingAfterUserLeft_DoesNotYankScreenBack's
// counterpart for the document review form.
func TestApp_ReviewSaveResolvingAfterUserLeft_DoesNotYankScreenBack(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("# Cover Letter"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
	app, _ = sendKey(app, runeKey('p'))                        // application detail -> posting detail
	app, _ = sendKey(app, runeKey('r'))                        // posting detail -> document review select
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // select "Cover Letter" (cursor 0)
	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after selecting cover letter = %v, want screenDocumentReviewForm", app.currentScreen)
	}
	app, saveCmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // pass
	if saveCmd == nil {
		t.Fatal("Update on ctrl+s returned nil Cmd, want a command that saves the review")
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // review form -> posting detail, save still in flight
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // posting detail -> application detail
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after backing out = %v, want screenApplicationDetail", app.currentScreen)
	}

	app = applyCmd(t, app, saveCmd)

	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after late save result = %v, want screenApplicationDetail (where the user already was)", app.currentScreen)
	}
}

func TestApp_ReviewSaveResolvingAfterReentry_KeepsReopenedScreenOpen(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(mustDoc(t, status, document.CoverLetter).Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, status, document.CoverLetter).Path, []byte("# Cover Letter"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})                 // active-applications -> application detail
	app, _ = sendKey(app, runeKey('p'))                                        // application detail -> posting detail
	app, _ = sendKey(app, runeKey('r'))                                        // posting detail -> document review select
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})                 // select "Cover Letter" (cursor 0)
	app, saveCmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // pass
	if saveCmd == nil {
		t.Fatal("Update on ctrl+s returned nil Cmd, want a command that saves the review")
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // review form -> posting detail, save still in flight
	app, _ = sendKey(app, runeKey('r'))                      // posting detail -> document review select again
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after re-entering = %v, want screenDocumentReviewForm", app.currentScreen)
	}

	app = applyCmd(t, app, saveCmd)

	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after the earlier instance's save result = %v, want screenDocumentReviewForm (the reopened one stays open)", app.currentScreen)
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after esc from the reopened screen = %v, want screenPostingDetail", app.currentScreen)
	}
}

// TestApp_NestedBackOut_RetracesPath backs out of a transient screen
// entered from posting detail, itself entered from application detail:
// each esc must land one step back along the path the user actually took,
// ending at application detail rather than the never-browsed posting list.
func TestApp_NestedBackOut_RetracesPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		key       rune
		transient screen
	}{
		{name: "status select", key: 's', transient: screenApplicationStatusSelect},
		{name: "document review select", key: 'r', transient: screenDocumentReviewSelect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
			mustCreateApplication(t, s, posting.ID)
			app := newTestApp(t, s, newTestSyncer(s, nil))

			app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // active-applications -> application detail
			app, _ = sendKey(app, runeKey('p'))                        // application detail -> posting detail
			app = sendKeyAndApply(t, app, runeKey(tt.key))
			if app.currentScreen != tt.transient {
				t.Fatalf("screen after %q = %v, want %v", tt.key, app.currentScreen, tt.transient)
			}

			for _, want := range []screen{screenPostingDetail, screenApplicationDetail, screenActiveApplications} {
				app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
				if app.currentScreen != want {
					t.Fatalf("screen after esc = %v, want %v", app.currentScreen, want)
				}
			}
		})
	}
}

// TestApp_NotesSaveResolvingAfterUserLeft_DoesNotYankScreenBack is
// TestApp_StatusSaveResolvingAfterUserLeft_DoesNotYankScreenBack's
// counterpart for the notes editor. It matters more here than it looks:
// being pulled back to posting detail after the user's own esc already
// popped its returnStack entry would leave the next esc with nowhere
// correct to go.
func TestApp_NotesEsc_WhileSaving_WaitsForTheSave(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	if _, err := createApplicationIn(context.Background(), s, app.postings[0].ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('n'))
	app, _ = sendKey(app, runeKey('H', 'i'))
	app, saveCmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if saveCmd == nil {
		t.Fatal("Update on ctrl+s (notes edit) returned nil Cmd, want a command that saves notes")
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // ignored: the save is still in flight
	if app.currentScreen != screenApplicationNotesEdit {
		t.Fatalf("screen after esc while saving = %v, want screenApplicationNotesEdit", app.currentScreen)
	}

	app = applyCmd(t, app, saveCmd)

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen after the save result = %v, want screenPostingDetail", app.currentScreen)
	}
}

func TestApp_NotesSaveResolvingAfterReentry_KeepsReopenedScreenOpen(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	if _, err := createApplicationIn(context.Background(), s, app.postings[0].ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('n'))
	earlier := app.applicationNotes.instance
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc}) // notes edit -> posting detail
	app, _ = sendKey(app, runeKey('n'))                      // posting detail -> notes edit again
	if app.currentScreen != screenApplicationNotesEdit {
		t.Fatalf("screen after re-entering = %v, want screenApplicationNotesEdit", app.currentScreen)
	}
	stored, err := s.Postings.Application(context.Background(), app.postings[0].ID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}

	app, _ = sendKey(app, applicationNotesUpdatedMsg{application: *stored, from: earlier})

	if app.currentScreen != screenApplicationNotesEdit {
		t.Fatalf("screen after the earlier instance's save result = %v, want screenApplicationNotesEdit (the reopened one stays open)", app.currentScreen)
	}
}

func TestApp_PostingList_ShowsSelectedCompanysDescription(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	const description = "Acme builds rockets for roadrunner enthusiasts."
	if _, err := s.Companies.SetDescription(context.Background(), acme.ID, description); err != nil {
		t.Fatalf("UpdateCompanyDescription: %v", err)
	}
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	if got := viewText(app); !strings.Contains(got, description) {
		t.Fatalf("posting list View missing company description %q:\n%s", description, got)
	}
}

func TestApp_PressR_UpdatesCompanyListOpenCountAndLastFetched(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}, {SourceID: "job-2", Title: "Designer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	if got := viewText(app); !strings.Contains(got, "never") {
		t.Fatalf("before refresh, company list should say never fetched:\n%s", got)
	}

	app, cmd := sendIntentKey(t, app, runeKey('r'))
	if cmd == nil {
		t.Fatal("Update on 'r' returned nil Cmd")
	}
	app, cmd = sendKey(app, refreshResult(t, cmd))
	if cmd == nil {
		t.Fatal("Update on the refresh result returned nil Cmd, want one that reloads companies")
	}
	app = applyCmd(t, app, cmd)

	got := viewText(app)
	if strings.Contains(got, "never") {
		t.Errorf("after refresh, company list still says never fetched:\n%s", got)
	}
	if !strings.Contains(got, "    2") {
		t.Errorf("after refresh, company list missing open count 2:\n%s", got)
	}
}

func isCompaniesLoaded(msg tea.Msg) bool {
	_, ok := msg.(message.CompaniesLoadedMsg)
	return ok
}

// Archiving changes a company's open count, so the company list reloads
// when you return to it rather than showing a stale number.
func TestApp_BackToCompanyList_AfterArchiving_ShowsUpdatedOpenCount(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}, {SourceID: "job-2", Title: "Designer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app, cmd := sendKey(app, runeKey('x'))
	if cmd == nil {
		t.Fatal("Update on 'x' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	app, cmd = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Update on esc back to the company list returned nil Cmd, want one that reloads companies")
	}
	app, _ = sendKey(app, cmd())

	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen = %v, want screenCompanyList", app.currentScreen)
	}
	if got := app.companyList.openPostings[app.companyList.companies[0].ID]; got != 1 {
		t.Errorf("open count after archiving one of two = %d, want 1", got)
	}
}

// TestApp_ApplicationDetail_EnterApplicationFormByHand: 'f' on
// application detail opens the posting's application form as text,
// seeded with a line per document type; ctrl+s stores what was typed the
// same way a fetched Greenhouse form is stored, and returns to detail.
func TestApp_ApplicationDetail_EnterApplicationFormByHand(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 30})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenApplicationDetail {
		t.Fatalf("screen after enter = %v, want screenApplicationDetail", app.currentScreen)
	}

	app, loadCmd := sendKey(app, runeKey('f'))
	if loadCmd == nil {
		t.Fatal("Update on 'f' returned nil Cmd, want a command that loads the stored form")
	}
	app = applyCmd(t, app, loadCmd)
	if app.currentScreen != screenApplicationForm {
		t.Fatalf("screen after 'f' = %v, want screenApplicationForm", app.currentScreen)
	}
	if view := viewText(app); !strings.Contains(view, "resume: absent") {
		t.Errorf("form screen doesn't show the seeded document lines:\n%s", view)
	}

	app, _ = sendKey(app, runeKey([]rune("Why us? *")...))
	app, saveCmd := sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if saveCmd == nil {
		t.Fatal("Update on ctrl+s returned nil Cmd, want a command that saves the form")
	}
	app = applyCmd(t, app, saveCmd)

	if app.err != nil {
		t.Fatalf("app.err = %v", app.err)
	}
	if app.currentScreen != screenApplicationDetail {
		t.Errorf("screen after saving = %v, want screenApplicationDetail", app.currentScreen)
	}
	stored, err := s.Postings.Form(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("Form: %v", err)
	}
	var got jobboard.ApplicationForm
	if err := json.Unmarshal([]byte(stored.JSON), &got); err != nil {
		t.Fatalf("decode stored form: %v", err)
	}
	want := jobboard.ApplicationForm{
		Documents: map[document.Type]jobboard.Requirement{},
		Questions: []jobboard.Question{{Label: "Why us?", Required: true}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("stored form mismatch (-want +got):\n%s", diff)
	}
}

// Searching the company list and pressing enter opens the highlighted
// match's postings; coming back finds the search as it was left.
func TestApp_CompanyList_SearchEnterEnter_OpensMatchAndKeepsSearchOnReturn(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	mustCreateCompany(t, s, "Initech", jobboard.Ashby, "initech")
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	for _, r := range "/glo" {
		app, _ = sendKey(app, runeKey(r))
	}
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = applyCmd(t, app, cmd)
	if app.currentScreen != screenPostingList {
		t.Fatalf("screen after enter, enter = %v, want screenPostingList", app.currentScreen)
	}
	if app.selectedCompany.Name != "Globex" {
		t.Fatalf("selectedCompany = %q, want %q", app.selectedCompany.Name, "Globex")
	}

	app, cmd = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	app = applyCmd(t, app, cmd)
	if app.currentScreen != screenCompanyList {
		t.Fatalf("screen after esc = %v, want screenCompanyList", app.currentScreen)
	}
	if view := viewText(app); !strings.Contains(view, "🔍 glo") || strings.Contains(view, "Initech") {
		t.Errorf("company list after coming back should still show the search for \"glo\":\n%s", view)
	}
}

// Narrowing the window makes the terminal rewrap the frame on screen,
// pushing its last rows below where the next frame ends; the renderer
// only clears below a frame that got shorter, so they'd stay there.
func TestApp_Resize_ClearsTheScreen(t *testing.T) {
	t.Parallel()

	app := New(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		documents.NewStore(t.TempDir()),
	)
	_, cmd := sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("resize returned no command, want tea.ClearScreen")
	}
	if got, want := cmd(), tea.ClearScreen(); got != want {
		t.Errorf("resize command produced %T, want %T (tea.ClearScreen)", got, want)
	}
}

// refreshResult runs r's command and returns the refresh's own result,
// without the spinner tick batched with it.
func refreshResult(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("r's command isn't a batch of the refresh and the spinner")
	}
	for _, c := range batch {
		if msg, ok := c().(message.CompanyRefreshedMsg); ok {
			return msg
		}
	}
	t.Fatal("r's command has no refresh in it")
	return nil
}

// TestApp_WithStatus_ShowsOnTheStatusLineAtStart: how the process started
// (e.g. the in-process MCP server, or why it couldn't) is the first thing
// on the status line.
func TestApp_WithStatus_ShowsOnTheStatusLineAtStart(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithStatus("MCP server on 127.0.0.1:8787")

	lines := strings.Split(ansi.Strip(viewText(app)), "\n")
	if last := lines[len(lines)-1]; last != "MCP server on 127.0.0.1:8787" {
		t.Errorf("last line = %q, want the start-up status", last)
	}
}

func TestApp_NotesSaveFails_UnblocksEditing(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	if _, err := createApplicationIn(context.Background(), s, app.postings[0].ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('n'))
	app, _ = sendKey(app, runeKey('H', 'i'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	app, _ = sendKey(app, applicationNotesUpdatedMsg{from: app.applicationNotes.instance, err: errors.New("disk full")})
	app, _ = sendKey(app, runeKey('!'))

	if app.currentScreen != screenApplicationNotesEdit {
		t.Fatalf("screen after failed save = %v, want screenApplicationNotesEdit", app.currentScreen)
	}
	if got := app.applicationNotes.textarea.Value(); got != "Hi!" {
		t.Fatalf("notes after failed save and typing = %q, want %q (editing resumes)", got, "Hi!")
	}
}

func TestApp_ApplicationFormSaveFails_UnblocksEditing(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 30})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, loadCmd := sendKey(app, runeKey('f'))
	app = applyCmd(t, app, loadCmd)
	app, _ = sendKey(app, runeKey([]rune("Why us? *")...))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	app, _ = sendKey(app, applicationFormSavedMsg{err: errors.New("disk full")})
	app, _ = sendKey(app, runeKey('!'))

	if app.currentScreen != screenApplicationForm {
		t.Fatalf("screen after failed save = %v, want screenApplicationForm", app.currentScreen)
	}
	if got := app.applicationForm.textarea.Value(); !strings.HasSuffix(got, "Why us? *!") {
		t.Fatalf("form after failed save and typing = %q, want it to end %q (editing resumes)", got, "Why us? *!")
	}
}

// TestApp_ApplicationFormEscWithEdits_DrawsDiscardBox: the discard prompt
// is the same box over the screen as the delete confirmations.
func TestApp_ApplicationFormEscWithEdits_DrawsDiscardBox(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 30})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, loadCmd := sendKey(app, runeKey('f'))
	app = applyCmd(t, app, loadCmd)
	app, _ = sendKey(app, runeKey([]rune("Why us? *")...))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	view := viewText(app)
	box := ansi.Strip(app.applicationForm.discardBox(app.width))
	for _, line := range strings.Split(box, "\n") {
		if !strings.Contains(view, line) {
			t.Fatalf("view doesn't draw the discard box line %q:\n%s", line, view)
		}
	}
	if !strings.Contains(box, "╭") || !strings.Contains(box, "Discard unsaved changes?") {
		t.Errorf("discard box isn't a confirm box asking to discard:\n%s", box)
	}
}

func TestApp_NotesEscWithEdits_DrawsDiscardBox(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	if _, err := createApplicationIn(context.Background(), s, app.postings[0].ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('n'))
	app, _ = sendKey(app, runeKey('H', 'i'))

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEsc})

	view := viewText(app)
	box := ansi.Strip(app.applicationNotes.discardBox(app.width))
	for _, line := range strings.Split(box, "\n") {
		if !strings.Contains(view, line) {
			t.Fatalf("view doesn't draw the discard box line %q:\n%s", line, view)
		}
	}
	if !strings.Contains(box, "╭") || !strings.Contains(box, "Discard unsaved changes?") {
		t.Errorf("discard box isn't a confirm box asking to discard:\n%s", box)
	}
}
