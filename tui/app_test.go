package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

// newTestApp creates an App and drives it through Init, returning the
// model with its initial companies already loaded. Tests that don't care
// about refresh behavior can pass a syncer with no configured postings.
// The App's documents.Store is rooted at a fresh t.TempDir() -- tests
// that care about it (i.e. application document status) can read it back
// via app.documents.
func newTestApp(t *testing.T, s *store.Store, syncer *sync.Syncer) *App {
	t.Helper()
	app := New(s, syncer, documents.NewStore(t.TempDir()))
	return applyCmd(t, app, app.Init())
}

// applyCmd runs cmd and feeds the resulting message through app.Update.
// If cmd is itself a tea.Batch (App.Init returns one, to load companies
// and active applications concurrently), its BatchMsg unpacks into
// per-command messages that a real tea.Program's runtime loop would run
// and apply individually -- these tests drive App directly without one,
// so this replicates that unpacking recursively.
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
	model, _ := app.Update(msg)
	return model.(*App)
}

// sendKey applies msg to app.Update and returns the resulting model (cast
// back to *App) and any Cmd, saving the repeated type-assertion at every
// call site.
func sendKey(app *App, msg tea.Msg) (*App, tea.Cmd) {
	model, cmd := app.Update(msg)
	return model.(*App), cmd
}

func runeKey(r ...rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}
}

// openPostingList drives app from wherever it starts (the active-
// applications home screen) into the posting list for the currently
// selected company: to the company list, refresh, then enter.
func openPostingList(t *testing.T, app *App) *App {
	t.Helper()
	app, _ = sendKey(app, runeKey('c'))
	app, cmd := sendKey(app, runeKey('r'))
	if cmd == nil {
		t.Fatal("Update on 'r' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	app, cmd = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())
	return app
}

// openPostingDetail drives app (already on screenPostingList) into the
// detail view for the currently selected posting, including running the
// command dispatched on entry that loads the posting's application (if
// any), so app.applicationsByPosting reflects DB state immediately.
func openPostingDetail(app *App) *App {
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		app, _ = sendKey(app, cmd())
	}
	return app
}

func TestApp_Init_LoadsCompanies(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	app := newTestApp(t, s, newTestSyncer(s, nil))

	if len(app.companies) != 1 {
		t.Fatalf("app.companies = %+v, want 1 company", app.companies)
	}
	if app.companies[0].ID != acme.ID {
		t.Fatalf("app.companies[0].ID = %d, want %d", app.companies[0].ID, acme.ID)
	}
}

func TestApp_CursorDown_MovesSelectionWithinBounds(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustCreateCompany(t, s, "Globex", "ashby", "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	if app.companyList.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", app.companyList.cursor)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	if app.companyList.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", app.companyList.cursor)
	}

	// At the bottom, another down should not move past the last company.
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	if app.companyList.cursor != 1 {
		t.Fatalf("cursor after down at bottom = %d, want 1 (clamped)", app.companyList.cursor)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyUp})
	if app.companyList.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", app.companyList.cursor)
	}
}

func TestApp_PressA_EntersCompanyForm(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	if app.screen != screenActiveApplications {
		t.Fatalf("initial screen = %v, want screenActiveApplications (the home screen)", app.screen)
	}

	app, _ = sendKey(app, runeKey('c'))
	if app.screen != screenCompanyList {
		t.Fatalf("screen after 'c' = %v, want screenCompanyList", app.screen)
	}

	app, _ = sendKey(app, runeKey('a'))

	if app.screen != screenCompanyForm {
		t.Fatalf("screen after 'a' = %v, want screenCompanyForm", app.screen)
	}
}

func TestApp_TypingInForm_UpdatesFocusedField(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab}) // source field is focused first; move to name
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))

	if got := app.companyForm.inputs[formFieldName].Value(); got != "Acme" {
		t.Fatalf("companyForm.inputs[formFieldName].Value() = %q, want %q", got, "Acme")
	}
}

func TestApp_Tab_MovesFocusToNextField(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	app, _ = sendKey(app, runeKey('a'))
	if app.companyForm.focus != formFieldSource {
		t.Fatalf("companyForm.focus on entering the form = %d, want formFieldSource", app.companyForm.focus)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab})
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab})

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
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab}) // source (default "ashby") -> name
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab}) // name -> sourceRef
	app, _ = sendKey(app, runeKey('a', 'c', 'm', 'e'))

	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that creates the company")
	}
	app, _ = sendKey(app, cmd())

	if app.screen != screenCompanyList {
		t.Fatalf("screen after submit = %v, want screenCompanyList", app.screen)
	}
	if len(app.companies) != 1 {
		t.Fatalf("app.companies = %+v, want 1 company", app.companies)
	}
	if app.companies[0].Name != "Acme" || app.companies[0].Source != "ashby" || app.companies[0].SourceRef != "acme" {
		t.Fatalf("created company = %+v, want Name=Acme Source=ashby SourceRef=acme", app.companies[0])
	}

	stored, err := s.ListActiveCompanies(context.Background())
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored companies = %+v, want 1", stored)
	}
}

func TestApp_PressD_DeletesSelectedCompany(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	app, cmd := sendKey(app, runeKey('d'))
	if cmd == nil {
		t.Fatal("Update on 'd' returned nil Cmd, want a command that deletes the company")
	}
	app, _ = sendKey(app, cmd())

	if len(app.companies) != 0 {
		t.Fatalf("app.companies after delete = %+v, want empty (soft-deleted, excluded from active list)", app.companies)
	}

	_, err := s.GetCompany(context.Background(), acme.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetCompany after delete error = %v, want ErrNotFound (soft-deleted companies are excluded)", err)
	}
}

func TestApp_PressE_EditsCompanyName(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	app, _ = sendKey(app, runeKey('e'))
	if app.screen != screenCompanyEdit {
		t.Fatalf("screen after 'e' = %v, want screenCompanyEdit", app.screen)
	}
	if got := app.companyEdit.nameInput.Value(); got != "Acme" {
		t.Fatalf("companyEdit.nameInput.Value() = %q, want %q (seeded from selected company)", got, "Acme")
	}

	app, _ = sendKey(app, runeKey(' ', 'C', 'o', 'r', 'p'))
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that saves the company name")
	}
	app, _ = sendKey(app, cmd())

	if app.screen != screenCompanyList {
		t.Fatalf("screen after submit = %v, want screenCompanyList", app.screen)
	}
	if len(app.companies) != 1 || app.companies[0].Name != "Acme Corp" {
		t.Fatalf("app.companies = %+v, want 1 company named %q", app.companies, "Acme Corp")
	}
	if app.companies[0].Source != "ashby" || app.companies[0].SourceRef != "acme" {
		t.Fatalf("app.companies[0] = %+v, want Source/SourceRef unchanged (ashby/acme)", app.companies[0])
	}

	stored, err := s.GetCompany(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Name != "Acme Corp" {
		t.Fatalf("stored.Name = %q, want %q", stored.Name, "Acme Corp")
	}
}

func TestApp_EscWhileCompanyNameSavePending_DoesNotRaceTheSave(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	app, _ = sendKey(app, runeKey('e'))
	app, _ = sendKey(app, runeKey(' ', 'C', 'o', 'r', 'p'))
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that saves the company name")
	}

	// A fast Esc raced against the still-in-flight save from above --
	// bubbletea can't cancel a dispatched command, so this must not
	// switch screens (that would claim "cancelled" while the save is
	// still going to land moments later).
	app, escCmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	if escCmd != nil {
		t.Fatalf("Update on esc while save pending returned %v, want nil (esc blocked)", escCmd)
	}
	if app.screen != screenCompanyEdit {
		t.Fatalf("screen after esc while save pending = %v, want screenCompanyEdit (still blocked)", app.screen)
	}

	// The save resolves; screen leaves on its own like a normal submit.
	app, _ = sendKey(app, cmd())
	if app.screen != screenCompanyList {
		t.Fatalf("screen after save resolved = %v, want screenCompanyList", app.screen)
	}

	stored, err := s.GetCompany(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Name != "Acme Corp" {
		t.Fatalf("stored.Name = %q, want %q (save was not cancelled)", stored.Name, "Acme Corp")
	}
}

func TestApp_SubmitForm_KeepsCompanyListSorted(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Globex", "ashby", "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	// "Acme" sorts before the already-loaded "Globex" -- appending it
	// blindly would leave the in-memory list out of order even though a
	// fresh load would show it first.
	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab}) // source -> name
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab}) // name -> sourceRef
	app, _ = sendKey(app, runeKey('a', 'c', 'm', 'e'))

	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that creates the company")
	}
	app, _ = sendKey(app, cmd())

	if len(app.companies) != 2 {
		t.Fatalf("app.companies = %+v, want 2 companies", app.companies)
	}
	if app.companies[0].Name != "Acme" || app.companies[1].Name != "Globex" {
		t.Fatalf("app.companies names = [%q, %q], want [Acme, Globex] (alphabetical)",
			app.companies[0].Name, app.companies[1].Name)
	}
}

func TestApp_PressE_RenameKeepsCompanyListSorted(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustCreateCompany(t, s, "Globex", "ashby", "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	// Renaming "Acme" (cursor at index 0) to "Zzz" moves it past "Globex"
	// alphabetically -- an in-place replace at the old index would leave
	// the list out of order even though a fresh load would show it last.
	app, _ = sendKey(app, runeKey('e'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyBackspace, Alt: false})
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyBackspace})
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyBackspace})
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyBackspace})
	app, _ = sendKey(app, runeKey('Z', 'z', 'z'))

	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on submit returned nil Cmd, want a command that saves the company name")
	}
	app, _ = sendKey(app, cmd())

	if len(app.companies) != 2 {
		t.Fatalf("app.companies = %+v, want 2 companies", app.companies)
	}
	if app.companies[0].Name != "Globex" || app.companies[1].Name != "Zzz" {
		t.Fatalf("app.companies names = [%q, %q], want [Globex, Zzz] (alphabetical)",
			app.companies[0].Name, app.companies[1].Name)
	}
}

func TestApp_PressI_OnPostingList_TogglesInterestedOnSelectedPosting(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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

	markup, err := s.GetPostingMarkup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
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

	markup, err = s.GetPostingMarkup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
	}
	if markup.InterestedAt != nil {
		t.Fatal("InterestedAt is non-nil after second 'i', want nil (toggled off)")
	}
}

func TestApp_PressX_OnPostingList_TogglesArchivedOnSelectedPosting(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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

	markup, err := s.GetPostingMarkup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
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

	markup, err = s.GetPostingMarkup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
	}
	if markup.ArchivedAt != nil {
		t.Fatal("ArchivedAt is non-nil after second 'x', want nil (toggled off)")
	}
}

func TestApp_PressI_WhileArchived_SwitchesToInterestedAndClearsArchived(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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

	markup, err := s.GetPostingMarkup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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

	markup, err := s.GetPostingMarkup(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, runeKey('c'))

	app, cmd := sendKey(app, runeKey('r'))
	if cmd == nil {
		t.Fatal("Update on 'r' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	postings, err := s.ListPostingsByCompany(context.Background(), app.companies[0].ID)
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
	if _, err := s.SetPostingArchived(context.Background(), archivedID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}

	app, cmd = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	if _, err := s.SetPostingArchived(context.Background(), archivedID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
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

func TestApp_PressX_WhileHidingArchived_RemovesPostingFromViewAndClampsCursor(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	app = openPostingDetail(app)

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on 'a' returned nil Cmd, want a command that creates the application")
	}
	_, _ = sendKey(app, cmd())

	application, err := s.GetApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if application.Status != store.ApplicationStatusStarted {
		t.Fatalf("application.Status = %s, want %s", application.Status, store.ApplicationStatusStarted)
	}
}

// TestApp_PressA_OnPostingDetail_NewApplication_AppearsInActiveApplications
// verifies a freshly-started application shows up in the active-
// applications list without needing a restart -- applicationCreatedMsg
// reloads it, the same way applicationStatusUpdatedMsg already does for
// status changes.
func TestApp_PressA_OnPostingDetail_NewApplication_AppearsInActiveApplications(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	if len(app.activeApplications) != 0 {
		t.Fatalf("app.activeApplications before starting one = %+v, want empty", app.activeApplications)
	}
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on 'a' returned nil Cmd, want a command that creates the application")
	}
	app, createdCmd := sendKey(app, cmd())
	if createdCmd == nil {
		t.Fatal("applicationCreatedMsg handling returned nil Cmd, want a command that reloads active applications")
	}
	app, _ = sendKey(app, createdCmd())

	if len(app.activeApplications) != 1 {
		t.Fatalf("app.activeApplications after starting one = %+v, want 1", app.activeApplications)
	}
}

func TestApp_PressA_OnPostingDetail_WhenApplicationAlreadyExists_DoesNotDuplicate(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	app = openPostingDetail(app)

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on first 'a' returned nil Cmd, want a command that creates the application")
	}
	app, _ = sendKey(app, cmd())

	_, cmd = sendKey(app, runeKey('a'))
	if cmd != nil {
		t.Fatal("Update on second 'a' (application already exists) returned non-nil Cmd, want nil (no-op)")
	}

	applications, err := s.GetApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if applications.Status != store.ApplicationStatusStarted {
		t.Fatalf("application.Status = %s, want %s (unchanged)", applications.Status, store.ApplicationStatusStarted)
	}
}

func TestApp_OpenPostingDetail_WithExistingApplication_LoadsAndDisplaysStatus(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := s.CreateApplication(context.Background(), postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	app = openPostingDetail(app)

	application, ok := app.applicationsByPosting[postingID]
	if !ok {
		t.Fatal("app.applicationsByPosting has no entry for postingID after opening detail, want the pre-existing application loaded")
	}
	if application.Status != store.ApplicationStatusStarted {
		t.Fatalf("loaded application.Status = %s, want %s", application.Status, store.ApplicationStatusStarted)
	}
	if !strings.Contains(app.postingDetail.viewport.View(), "application_started") {
		t.Fatalf("detail viewport view = %q, want it to contain the application status", app.postingDetail.viewport.View())
	}
}

func TestApp_OpenPostingDetail_WithNoApplication_ShowsNoApplicationMessage(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	if strings.Contains(app.postingDetail.viewport.View(), "application_started") {
		t.Fatalf("detail viewport view = %q, want no application status shown", app.postingDetail.viewport.View())
	}
}

func TestApp_PressS_OnPostingDetail_WithApplication_OpensStatusSelect(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := s.CreateApplication(context.Background(), postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	app = openPostingDetail(app)

	app, _ = sendKey(app, runeKey('s'))

	if app.screen != screenApplicationStatusSelect {
		t.Fatalf("screen after 's' = %v, want screenApplicationStatusSelect", app.screen)
	}
}

func TestApp_PressS_OnPostingDetail_WithNoApplication_NoOp(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, _ = sendKey(app, runeKey('s'))

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after 's' with no application = %v, want screenPostingDetail (no-op)", app.screen)
	}
}

func TestApp_StatusSelect_Enter_UpdatesStatusAndReturnsToDetail(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := s.CreateApplication(context.Background(), postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	app = openPostingDetail(app)
	app, _ = sendKey(app, runeKey('s'))

	// Cursor starts at 0 ("application_started"); move down once to land on
	// "application_submitted" (the second entry in applicationStatuses).
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter (status select) returned nil Cmd, want a command that updates the status")
	}
	app, _ = sendKey(app, cmd())

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after saving status = %v, want screenPostingDetail", app.screen)
	}
	if app.applicationsByPosting[postingID].Status != store.ApplicationStatusSubmitted {
		t.Fatalf("app.applicationsByPosting[postingID].Status = %s, want %s", app.applicationsByPosting[postingID].Status, store.ApplicationStatusSubmitted)
	}

	stored, err := s.GetApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if stored.Status != store.ApplicationStatusSubmitted {
		t.Fatalf("stored application.Status = %s, want %s", stored.Status, store.ApplicationStatusSubmitted)
	}
}

func TestApp_StatusSelect_Esc_CancelsWithoutSaving(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := s.CreateApplication(context.Background(), postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	app = openPostingDetail(app)
	app, _ = sendKey(app, runeKey('s'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after esc = %v, want screenPostingDetail", app.screen)
	}
	stored, err := s.GetApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if stored.Status != store.ApplicationStatusStarted {
		t.Fatalf("stored application.Status = %s, want %s (esc should not persist)", stored.Status, store.ApplicationStatusStarted)
	}
}

func TestApp_StartsOnActiveApplicationsScreen(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)

	app := newTestApp(t, s, newTestSyncer(s, nil))

	if app.screen != screenActiveApplications {
		t.Fatalf("initial screen = %v, want screenActiveApplications", app.screen)
	}
	if len(app.activeApplications) != 1 {
		t.Fatalf("app.activeApplications = %+v, want 1 (loaded on Init)", app.activeApplications)
	}
	if app.activeApplications[0].CompanyName != "Acme" {
		t.Fatalf("activeApplications[0].CompanyName = %q, want %q", app.activeApplications[0].CompanyName, "Acme")
	}
}

func TestApp_ActiveApplications_C_ThenEsc_ReturnsHome(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, runeKey('c'))
	if app.screen != screenCompanyList {
		t.Fatalf("screen after 'c' = %v, want screenCompanyList", app.screen)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	if app.screen != screenActiveApplications {
		t.Fatalf("screen after esc from company list = %v, want screenActiveApplications", app.screen)
	}
}

// TestApp_ActiveApplications_StatusChange_ReturnsToActiveApplications
// exercises the return-screen tracking applicationStatusReturnScreen
// exists for: screenApplicationStatusSelect is reachable from both
// screenPostingDetail and screenActiveApplications now, so saving (or
// cancelling) must land back on whichever one entered it, not a
// hardcoded screen.
func TestApp_ActiveApplications_StatusChange_ReturnsToActiveApplications(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, runeKey('s'))
	if app.screen != screenApplicationStatusSelect {
		t.Fatalf("screen after 's' = %v, want screenApplicationStatusSelect", app.screen)
	}

	// Move from "application_started" (index 0) to "application_submitted".
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter (status select) returned nil Cmd, want a command that updates the status")
	}
	app, _ = sendKey(app, cmd())

	if app.screen != screenActiveApplications {
		t.Fatalf("screen after saving status = %v, want screenActiveApplications (where 's' was pressed from)", app.screen)
	}
}

// TestApp_ActiveApplications_StatusChangeToRejected_RemovesFromList
// verifies the applicationStatusUpdatedMsg handler reloads the active
// list after a status change, since moving to a terminal status
// (rejected/offer_declined) should drop the application from view
// immediately rather than on the next full reload.
func TestApp_ActiveApplications_StatusChangeToRejected_RemovesFromList(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, runeKey('s'))
	// Move from "application_started" (index 0) to "rejected" (index 3 --
	// started, submitted, interviewing, rejected).
	for range 3 {
		app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := s.CreateApplication(context.Background(), postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := s.UpdateApplicationNotes(context.Background(), postingID, "Referred by Alice"); err != nil {
		t.Fatalf("UpdateApplicationNotes: %v", err)
	}
	app = openPostingDetail(app)

	app, _ = sendKey(app, runeKey('n'))

	if app.screen != screenApplicationNotesEdit {
		t.Fatalf("screen after 'n' = %v, want screenApplicationNotesEdit", app.screen)
	}
	if got := app.applicationNotes.textarea.Value(); got != "Referred by Alice" {
		t.Fatalf("applicationNotes.textarea.Value() = %q, want %q", got, "Referred by Alice")
	}
}

func TestApp_NotesEdit_CtrlS_SavesNotesAndReturnsToDetail(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := s.CreateApplication(context.Background(), postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	app = openPostingDetail(app)
	app, _ = sendKey(app, runeKey('n'))

	app, _ = sendKey(app, runeKey('H', 'i'))
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("Update on ctrl+s (notes edit) returned nil Cmd, want a command that saves notes")
	}
	app, _ = sendKey(app, cmd())

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after saving notes = %v, want screenPostingDetail", app.screen)
	}
	if app.applicationsByPosting[postingID].Notes != "Hi" {
		t.Fatalf("app.applicationsByPosting[postingID].Notes = %q, want %q", app.applicationsByPosting[postingID].Notes, "Hi")
	}

	stored, err := s.GetApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if stored.Notes != "Hi" {
		t.Fatalf("stored application.Notes = %q, want %q", stored.Notes, "Hi")
	}
}

func TestApp_NotesEdit_Esc_CancelsWithoutSaving(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	postingID := app.postings[0].ID
	if _, err := s.CreateApplication(context.Background(), postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	app = openPostingDetail(app)
	app, _ = sendKey(app, runeKey('n'))

	app, _ = sendKey(app, runeKey('H', 'i'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after esc = %v, want screenPostingDetail", app.screen)
	}
	stored, err := s.GetApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if stored.Notes != "" {
		t.Fatalf("stored application.Notes = %q, want empty (esc should not persist)", stored.Notes)
	}
}

func TestApp_PressN_OnPostingDetail_WithNoApplication_NoOp(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, _ = sendKey(app, runeKey('n'))

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after 'n' with no application = %v, want screenPostingDetail (no-op)", app.screen)
	}
}

func TestApp_PressA_OnPostingDetail_CreatedApplication_ReflectedInDetailView(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)
	if strings.Contains(app.postingDetail.viewport.View(), "application_started") {
		t.Fatal("detail view shows application_started before creating an application")
	}

	app, cmd := sendKey(app, runeKey('a'))
	if cmd == nil {
		t.Fatal("Update on 'a' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	if !strings.Contains(app.postingDetail.viewport.View(), "application_started") {
		t.Fatalf("detail view after creating application = %q, want it to contain the new status", app.postingDetail.viewport.View())
	}
}

func TestApp_PostingDetail_NavigatingBetweenPostings_LoadsEachPostingsOwnApplication(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	if _, err := s.CreateApplication(context.Background(), designerID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := s.UpdateApplicationStatus(context.Background(), designerID, store.ApplicationStatusInterviewing); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}
	app = openPostingDetail(app) // opens the posting at cursor 0, no application

	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyRight})
	if cmd == nil {
		t.Fatal("Update on right (posting detail nav) returned nil Cmd, want a command that loads the next posting's application")
	}
	app, _ = sendKey(app, cmd())

	if !strings.Contains(app.postingDetail.viewport.View(), "interviewing") {
		t.Fatalf("detail view for Designer = %q, want it to contain %q", app.postingDetail.viewport.View(), "interviewing")
	}
	if _, ok := app.applicationsByPosting[engineerID]; ok {
		t.Fatal("applicationsByPosting has an entry for Engineer, want none (it has no application)")
	}
}

func TestApp_PressQ_ReturnsQuitCmd(t *testing.T) {
	s := newTestStore(t)
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
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.screen != screenCompanyList {
		t.Fatalf("screen after esc = %v, want screenCompanyList", app.screen)
	}
	if len(app.companies) != 0 {
		t.Fatalf("app.companies after cancel = %+v, want empty (nothing submitted)", app.companies)
	}
}

func TestApp_CancellingAScreen_ClearsStaleError(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

	app, _ = sendKey(app, runeKey('a'))
	// Simulate an error left over from a failed action on this screen
	// (e.g. a save that failed) -- a real failure would set this via a
	// tea.Msg, but the point under test is what happens to it on the
	// next screen transition, not how it got set.
	app.err = errors.New("boom")

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.screen != screenCompanyList {
		t.Fatalf("screen after esc = %v, want screenCompanyList", app.screen)
	}
	if app.err != nil {
		t.Fatalf("app.err after cancelling out = %v, want nil (stale error should clear on screen transition)", app.err)
	}
}

func TestApp_PressR_RefreshesSelectedCompanyAndShowsStatus(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, runeKey('c'))

	app, cmd := sendKey(app, runeKey('r'))
	if cmd == nil {
		t.Fatal("Update on 'r' returned nil Cmd, want a command that refreshes the company")
	}
	app, _ = sendKey(app, cmd())

	if app.status == "" {
		t.Fatal("app.status should be set after refresh")
	}
	if app.err != nil {
		t.Fatalf("app.err = %v, want nil", app.err)
	}

	postings, err := s.ListPostingsByCompany(context.Background(), app.companies[0].ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("stored postings = %+v, want 1", postings)
	}
}

func TestApp_PressEnter_OpensPostingListForSelectedCompany(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering"}},
	})
	app := newTestApp(t, s, syncer)

	app = openPostingList(t, app)

	if app.screen != screenPostingList {
		t.Fatalf("screen after enter = %v, want screenPostingList", app.screen)
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.screen != screenCompanyList {
		t.Fatalf("screen after esc = %v, want screenCompanyList", app.screen)
	}
}

func TestApp_PostingListCursor_MovesWithinBounds(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	if app.postingList.cursor != 1 {
		t.Fatalf("postingCursor after down = %d, want 1", app.postingList.cursor)
	}
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	if app.postingList.cursor != 1 {
		t.Fatalf("postingCursor after down at bottom = %d, want 1 (clamped)", app.postingList.cursor)
	}
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyUp})
	if app.postingList.cursor != 0 {
		t.Fatalf("postingCursor after up = %d, want 0", app.postingList.cursor)
	}
}

func TestApp_PressEnter_OnPostingList_OpensDetailForSelectedPosting(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app = openPostingDetail(app)

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after enter on posting list = %v, want screenPostingDetail", app.screen)
	}
}

func TestApp_PressEsc_OnPostingDetail_ReturnsToPostingList(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.screen != screenPostingList {
		t.Fatalf("screen after esc on detail = %v, want screenPostingList", app.screen)
	}
}

func TestApp_PostingDetail_RightMovesToNextPostingStayingInDetail(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	// ListPostingsByCompany orders first_seen_at DESC, id DESC -- both
	// postings share a first_seen_at (one sync batch), so the
	// later-inserted job-2/Designer (higher id) sorts first. Entering
	// detail from the top of the list starts on Designer.
	if app.postingDetail.posting.Title != "Designer" {
		t.Fatalf("initial posting shown in detail = %q, want %q", app.postingDetail.posting.Title, "Designer")
	}

	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyRight})

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after right on detail = %v, want screenPostingDetail (stay in detail)", app.screen)
	}
	if app.postingDetail.posting.Title != "Engineer" {
		t.Fatalf("posting shown after right on detail = %q, want %q", app.postingDetail.posting.Title, "Engineer")
	}
	if cmd == nil {
		t.Fatal("Update on right returned nil Cmd, want a command that loads the new posting's application")
	}
	app, _ = sendKey(app, cmd())

	// Clamped at the last posting.
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyRight})
	if app.postingDetail.posting.Title != "Engineer" {
		t.Fatalf("posting shown after right at last posting = %q, want %q (clamped)", app.postingDetail.posting.Title, "Engineer")
	}

	app, cmd = sendKey(app, tea.KeyMsg{Type: tea.KeyLeft})
	if app.postingDetail.posting.Title != "Designer" {
		t.Fatalf("posting shown after left on detail = %q, want %q", app.postingDetail.posting.Title, "Designer")
	}
	if cmd == nil {
		t.Fatal("Update on left returned nil Cmd, want a command that loads the new posting's application")
	}
	app, _ = sendKey(app, cmd())

	// Returning to the list should land the cursor on the posting last
	// viewed in detail, not wherever it was before entering detail.
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	if app.postingList.cursor != 0 {
		t.Fatalf("postingList.cursor after returning from detail = %d, want 0 (synced to last-viewed posting)", app.postingList.cursor)
	}
}

func TestApp_PostingDetail_DownScrollsLongDescription(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	longDescription := strings.Repeat("line\n", 100)
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longDescription}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	if app.postingDetail.viewport.YOffset != 0 {
		t.Fatalf("initial YOffset = %d, want 0", app.postingDetail.viewport.YOffset)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})

	if app.postingDetail.viewport.YOffset == 0 {
		t.Fatal("YOffset after down on a long description should have scrolled past 0")
	}
	if app.screen != screenPostingDetail {
		t.Fatalf("screen after down on detail = %v, want screenPostingDetail", app.screen)
	}
}

func TestApp_PostingDetail_VimJ_ScrollsLikeDown(t *testing.T) {
	// bubbles/viewport's DefaultKeyMap already binds j/k/h/l alongside the
	// arrow keys, so this should work with no changes on our side --
	// verifying that rather than assuming it.
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	longDescription := strings.Repeat("line\n", 100)
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longDescription}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, _ = sendKey(app, runeKey('j'))

	if app.postingDetail.viewport.YOffset == 0 {
		t.Fatal("YOffset after 'j' on a long description should have scrolled past 0")
	}
}

func TestApp_PostingDetail_ApplicationExistsWithFiles_ShowsFoundStatus(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	application, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	status := app.documents.Status(application.ID)
	if err := os.MkdirAll(filepath.Dir(status.CoverLetter.Path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(status.CoverLetter.Path, []byte("# Cover Letter"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}
	if err := os.WriteFile(status.Resume.Path, []byte("# Resume"), 0o644); err != nil {
		t.Fatalf("WriteFile resume: %v", err)
	}

	app = openPostingDetail(app)

	view := app.View()
	if !strings.Contains(view, "found ("+status.CoverLetter.Path+")") {
		t.Errorf("view does not show cover letter found with path %q:\n%s", status.CoverLetter.Path, view)
	}
	if !strings.Contains(view, "found ("+status.Resume.Path+")") {
		t.Errorf("view does not show resume found with path %q:\n%s", status.Resume.Path, view)
	}
}

func TestApp_PostingDetail_ApplicationExistsNoFiles_ShowsNotFoundStatus(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)

	posting := app.postings[0]
	application, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	// Deliberately no files written -- the application row exists, but
	// the documents don't yet.
	status := app.documents.Status(application.ID)

	app = openPostingDetail(app)

	view := app.View()
	if !strings.Contains(view, "not found ("+status.CoverLetter.Path+")") {
		t.Errorf("view does not show cover letter not found with path %q:\n%s", status.CoverLetter.Path, view)
	}
	if !strings.Contains(view, "not found ("+status.Resume.Path+")") {
		t.Errorf("view does not show resume not found with path %q:\n%s", status.Resume.Path, view)
	}
}

func TestApp_PostingDetail_NoApplication_ShowsNoDocumentsSection(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)
	// No CreateApplication call -- store.GetApplication will return
	// store.ErrNotFound, per the "no application -> show nothing"
	// decision (see decisions.log).

	app = openPostingDetail(app)

	view := app.View()
	if strings.Contains(view, "Documents") {
		t.Errorf("view shows a Documents section with no Application row:\n%s", view)
	}
	if strings.Contains(view, "Cover Letter") || strings.Contains(view, "Resume") {
		t.Errorf("view mentions cover letter/resume with no Application row:\n%s", view)
	}
}

func TestApp_CompanyList_VimJK_MoveCursorLikeArrows(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustCreateCompany(t, s, "Globex", "ashby", "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, runeKey('c'))

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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(app)

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
	if app.screen != screenPostingDetail {
		t.Fatalf("screen after 'l' = %v, want screenPostingDetail (stay in detail)", app.screen)
	}

	app, _ = sendKey(app, runeKey('h'))
	if app.postingDetail.posting.Title != "Designer" {
		t.Fatalf("posting shown after 'h' = %q, want %q", app.postingDetail.posting.Title, "Designer")
	}
}

func TestApp_PostingDetail_LongLine_WrapsToViewportWidth(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	longLine := strings.Repeat("word ", 40)
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longLine}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 20, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	for _, line := range strings.Split(app.postingDetail.viewport.View(), "\n") {
		if w := lipgloss.Width(line); w > 20 {
			t.Fatalf("rendered line %q has width %d, want <= 20", line, w)
		}
	}
}

func TestApp_WindowResize_OnPostingDetail_ReWraps(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	app = openPostingDetail(app)

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
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 24})

	if app.width != 80 || app.height != 24 {
		t.Fatalf("app.width, app.height = %d, %d, want 80, 24", app.width, app.height)
	}
}

func TestApp_PressO_OnPostingDetail_OpensJobURLInBrowser(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", JobURL: "https://jobs.ashbyhq.com/acme/job-1"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	_, cmd := sendKey(app, runeKey('o'))
	if cmd == nil {
		t.Fatal("Update on 'o' returned nil Cmd, want a command that opens the browser")
	}
}

func TestApp_PressO_OnPostingList_OpensSelectedPostingJobURLInBrowser(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app, cmd := sendKey(app, runeKey('f'))
	if cmd == nil {
		t.Fatal("Update on 'f' returned nil Cmd, want a command that loads filter options")
	}
	if app.screen != screenFilterSelect {
		t.Fatalf("screen after 'f' = %v, want screenFilterSelect", app.screen)
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeySpace})
	if !app.filterSelect.selectedDepartments["Engineering"] {
		t.Fatal("Engineering should be selected after space")
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeySpace})
	if app.filterSelect.selectedDepartments["Engineering"] {
		t.Fatal("Engineering should be deselected after second space")
	}
}

func TestApp_FilterSelect_Esc_CancelsWithoutSaving(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app, cmd := sendKey(app, runeKey('f'))
	app, _ = sendKey(app, cmd())

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeySpace})
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.screen != screenPostingList {
		t.Fatalf("screen after esc = %v, want screenPostingList", app.screen)
	}

	saved, err := s.ListCompanyFilters(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(saved) != 0 {
		t.Fatalf("saved filters = %+v, want none (esc should not persist selection)", saved)
	}
}

func TestApp_FilterSelect_Enter_SavesPersistsAndNarrowsAndReSyncs(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeySpace})

	app, cmd = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update on enter (save) returned nil Cmd, want a command that saves filters")
	}
	app, cmd = sendKey(app, cmd())
	if app.screen != screenPostingList {
		t.Fatalf("screen after save = %v, want screenPostingList", app.screen)
	}
	if cmd == nil {
		t.Fatal("Update on companyFiltersSavedMsg returned nil Cmd, want a command that re-syncs")
	}

	// Narrowed immediately, before the re-sync Cmd even runs.
	if len(app.postings) != 1 {
		t.Fatalf("postings after save = %+v, want 1 (narrowed to %s)", app.postings, wantDept)
	}
	if app.postings[0].Department != wantDept {
		t.Fatalf("remaining posting department = %q, want %q", app.postings[0].Department, wantDept)
	}

	saved, err := s.ListCompanyFilters(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(saved) != 1 || saved[0].Field != filter.FieldDepartment || saved[0].Value != wantDept {
		t.Fatalf("saved filters = %+v, want one department=%s filter", saved, wantDept)
	}

	// Drive the rest of the chain: the re-sync Cmd runs (hits the fake
	// fetcher again, no new data), producing companyRefreshedMsg, whose
	// handler reloads postings from the DB since this is the company
	// currently being viewed. That reload must NOT silently undo the
	// narrowing by loading everything unfiltered -- ListPostingsByCompany
	// itself has no notion of company_filters, so the reload has to
	// re-apply them, not just re-fetch.
	app, cmd = sendKey(app, cmd())
	if cmd == nil {
		t.Fatal("Update on companyRefreshedMsg returned nil Cmd, want a command that reloads postings")
	}
	app, _ = sendKey(app, cmd())

	if len(app.postings) != 1 {
		t.Fatalf("postings after post-sync reload = %+v, want still 1 (filter should survive the reload)", app.postings)
	}
	if app.postings[0].Department != wantDept {
		t.Fatalf("posting department after reload = %q, want %q", app.postings[0].Department, wantDept)
	}
}

func TestApp_CompanyRefreshed_ForCurrentlyViewedCompany_ReloadsPostings(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	// Simulate the background re-sync (triggered by saving a filter)
	// completing for the company currently being viewed.
	app, cmd := sendKey(app, companyRefreshedMsg{
		companyName: acme.Name,
		result:      sync.Result{CompanyID: acme.ID},
	})
	if cmd == nil {
		t.Fatal("Update on companyRefreshedMsg for the viewed company returned nil Cmd, want a command that reloads postings")
	}

	app, _ = sendKey(app, cmd())
	if len(app.postings) != 1 {
		t.Fatalf("postings after reload = %+v, want 1", app.postings)
	}
}

func TestApp_CompanyRefreshed_ForDifferentCompany_DoesNotReloadPostings(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	_, cmd := sendKey(app, companyRefreshedMsg{
		companyName: "SomeOtherCompany",
		result:      sync.Result{CompanyID: 999999},
	})
	if cmd != nil {
		t.Fatal("Update on companyRefreshedMsg for a different company returned a non-nil Cmd, want nil (shouldn't reload)")
	}
}

func TestApp_OpenPostingList_WithPreExistingSavedFilters_NarrowsOnFirstLoad(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(context.Background(), acme.ID, filter.FieldDepartment, "Engineering"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
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
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(context.Background(), acme.ID, filter.FieldDepartment, "Engineering"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	if _, err := s.CreateCompanyFilter(context.Background(), acme.ID, filter.FieldLocation, "Remote"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
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
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeySpace})
	app, cmd = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, _ = sendKey(app, cmd()) // companyFiltersSavedMsg

	if diff := cmp.Diff([]string{wantDept}, app.activeFilterDepartments); diff != "" {
		t.Fatalf("activeFilterDepartments after save mismatch (-want +got):\n%s", diff)
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

// TestFilterPostingsByCompanyFilters_MatchSemantics pins the same
// AND-across-fields/OR-within-field semantics SyncCompany's ingestion-time
// gating relies on (both now share sync.FilterRules -- see decisions.log,
// #61), via filterPostingsByCompanyFilters directly rather than through a
// full App.
func TestFilterPostingsByCompanyFilters_MatchSemantics(t *testing.T) {
	t.Parallel()

	eng := store.Posting{ID: 1, IngestedFields: store.IngestedFields{Department: "Engineering", Location: "Remote"}}
	sales := store.Posting{ID: 2, IngestedFields: store.IngestedFields{Department: "Sales", Location: "Remote"}}
	engNYC := store.Posting{ID: 3, IngestedFields: store.IngestedFields{Department: "Engineering", Location: "New York"}}
	postings := []store.Posting{eng, sales, engNYC}

	tests := []struct {
		name    string
		filters []store.CompanyFilter
		want    []store.Posting
	}{
		{
			name:    "no filters matches everything",
			filters: nil,
			want:    postings,
		},
		{
			name:    "department filter alone",
			filters: []store.CompanyFilter{{Field: filter.FieldDepartment, Value: "Engineering"}},
			want:    []store.Posting{eng, engNYC},
		},
		{
			name:    "location filter alone",
			filters: []store.CompanyFilter{{Field: filter.FieldLocation, Value: "Remote"}},
			want:    []store.Posting{eng, sales},
		},
		{
			name: "department and location AND together, OR within a field",
			filters: []store.CompanyFilter{
				{Field: filter.FieldDepartment, Value: "Engineering"},
				{Field: filter.FieldDepartment, Value: "Sales"},
				{Field: filter.FieldLocation, Value: "Remote"},
			},
			want: []store.Posting{eng, sales},
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

// TestFilterPostingsByCompanyFilters_UnsupportedField_ReturnsError guards
// a real behavior change from the old loadPostings path: splitCompanyFilters
// silently dropped any CompanyFilter row whose Field wasn't "department"/
// "location" (its switch has no default case), so an unrecognized field
// never even reached filter.Match -- that constraint was just silently
// not applied. filterPostingsByCompanyFilters instead routes every row
// through sync.FilterRules + filter.Match, the same path SyncCompany uses,
// so an unsupported field name -- which should be unreachable given
// company_filters' CHECK constraint, but this package can't assume that
// -- now surfaces as an error, consistent with ingestion-time gating,
// instead of silently narrowing the list as if the filter didn't exist.
func TestFilterPostingsByCompanyFilters_UnsupportedField_ReturnsError(t *testing.T) {
	t.Parallel()

	postings := []store.Posting{{ID: 1, IngestedFields: store.IngestedFields{Department: "Engineering"}}}
	filters := []store.CompanyFilter{{Field: "not_a_real_field", Value: "x"}}

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
// decisions.log, #61) rather than something this test argues should
// change -- it exists so a future change to this behavior is a visible,
// intentional diff instead of a silent one.
func TestApp_PostingsLoadedMsgWithErr_ClearsPostingsAndMarkup(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
