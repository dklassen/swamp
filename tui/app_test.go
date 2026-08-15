package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/ashby"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

// newTestApp creates an App and drives it through Init, returning the
// model with its initial companies already loaded. Tests that don't care
// about refresh behavior can pass a syncer with no configured postings.
func newTestApp(t *testing.T, s *store.Store, syncer *sync.Syncer) *App {
	t.Helper()
	app := New(s, syncer)
	model, _ := app.Update(app.Init()())
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

// openPostingList drives app from the company list into the posting list
// for the currently selected company: refresh, then enter.
func openPostingList(t *testing.T, app *App) *App {
	t.Helper()
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
// detail view for the currently selected posting.
func openPostingDetail(app *App) *App {
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
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

	if app.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", app.cursor)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	if app.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", app.cursor)
	}

	// At the bottom, another down should not move past the last company.
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	if app.cursor != 1 {
		t.Fatalf("cursor after down at bottom = %d, want 1 (clamped)", app.cursor)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyUp})
	if app.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", app.cursor)
	}
}

func TestApp_PressA_EntersCompanyForm(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	if app.screen != screenCompanyList {
		t.Fatalf("initial screen = %v, want screenCompanyList", app.screen)
	}

	app, _ = sendKey(app, runeKey('a'))

	if app.screen != screenCompanyForm {
		t.Fatalf("screen after 'a' = %v, want screenCompanyForm", app.screen)
	}
}

func TestApp_TypingInForm_UpdatesFocusedField(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))

	if got := app.formInputs[0].Value(); got != "Acme" {
		t.Fatalf("formInputs[0].Value() = %q, want %q", got, "Acme")
	}
}

func TestApp_Tab_MovesFocusToNextField(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab})

	if app.formFocus != formFieldSourceRef {
		t.Fatalf("formFocus after tab = %d, want formFieldSourceRef", app.formFocus)
	}
	if !app.formInputs[formFieldSourceRef].Focused() {
		t.Fatal("formInputs[formFieldSourceRef] should be focused after tab")
	}
	if app.formInputs[formFieldName].Focused() {
		t.Fatal("formInputs[formFieldName] should be blurred after tab")
	}

	app, _ = sendKey(app, runeKey('a', 'c', 'm', 'e'))
	if got := app.formInputs[formFieldSourceRef].Value(); got != "acme" {
		t.Fatalf("formInputs[formFieldSourceRef].Value() = %q, want %q", got, "acme")
	}
}

func TestApp_SubmitForm_CreatesCompanyAndReturnsToList(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab})
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
	if app.companies[0].Name != "Acme" || app.companies[0].SourceRef != "acme" {
		t.Fatalf("created company = %+v, want Name=Acme SourceRef=acme", app.companies[0])
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

func TestApp_PressI_OnPostingList_TogglesInterestedOnSelectedPosting(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)

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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	app.postingCursor = 1

	app, cmd := sendKey(app, runeKey('x'))
	if cmd == nil {
		t.Fatal("Update on 'x' returned nil Cmd")
	}
	app, _ = sendKey(app, cmd())

	if len(app.postings) != 1 {
		t.Fatalf("postings after archiving = %d, want 1 (archived one removed immediately)", len(app.postings))
	}
	if app.postingCursor != 0 {
		t.Fatalf("postingCursor after archiving = %d, want 0 (clamped)", app.postingCursor)
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

func TestApp_PressR_RefreshesSelectedCompanyAndShowsStatus(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)

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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	if app.postingCursor != 0 {
		t.Fatalf("initial postingCursor = %d, want 0", app.postingCursor)
	}
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	if app.postingCursor != 1 {
		t.Fatalf("postingCursor after down = %d, want 1", app.postingCursor)
	}
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	if app.postingCursor != 1 {
		t.Fatalf("postingCursor after down at bottom = %d, want 1 (clamped)", app.postingCursor)
	}
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyUp})
	if app.postingCursor != 0 {
		t.Fatalf("postingCursor after up = %d, want 0", app.postingCursor)
	}
}

func TestApp_PressEnter_OnPostingList_OpensDetailForSelectedPosting(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyRight})

	if app.screen != screenPostingDetail {
		t.Fatalf("screen after right on detail = %v, want screenPostingDetail (stay in detail)", app.screen)
	}
	if app.postingCursor != 1 {
		t.Fatalf("postingCursor after right on detail = %d, want 1", app.postingCursor)
	}

	// Clamped at the last posting.
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyRight})
	if app.postingCursor != 1 {
		t.Fatalf("postingCursor after right at last posting = %d, want 1 (clamped)", app.postingCursor)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyLeft})
	if app.postingCursor != 0 {
		t.Fatalf("postingCursor after left on detail = %d, want 0", app.postingCursor)
	}
}

func TestApp_PostingDetail_DownScrollsLongDescription(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	longDescription := strings.Repeat("line\n", 100)
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longDescription}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	if app.detailViewport.YOffset != 0 {
		t.Fatalf("initial YOffset = %d, want 0", app.detailViewport.YOffset)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})

	if app.detailViewport.YOffset == 0 {
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longDescription}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, _ = sendKey(app, runeKey('j'))

	if app.detailViewport.YOffset == 0 {
		t.Fatal("YOffset after 'j' on a long description should have scrolled past 0")
	}
}

func TestApp_CompanyList_VimJK_MoveCursorLikeArrows(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustCreateCompany(t, s, "Globex", "ashby", "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, _ = sendKey(app, runeKey('j'))
	if app.cursor != 1 {
		t.Fatalf("cursor after 'j' = %d, want 1", app.cursor)
	}
	app, _ = sendKey(app, runeKey('k'))
	if app.cursor != 0 {
		t.Fatalf("cursor after 'k' = %d, want 0", app.cursor)
	}
}

func TestApp_PostingList_VimJK_MoveCursorLikeArrows(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)

	app, _ = sendKey(app, runeKey('j'))
	if app.postingCursor != 1 {
		t.Fatalf("postingCursor after 'j' = %d, want 1", app.postingCursor)
	}
	app, _ = sendKey(app, runeKey('k'))
	if app.postingCursor != 0 {
		t.Fatalf("postingCursor after 'k' = %d, want 0", app.postingCursor)
	}
}

func TestApp_PostingDetail_VimHL_MovesBetweenPostingsLikeArrows(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {
			{SourceID: "job-1", Title: "Engineer"},
			{SourceID: "job-2", Title: "Designer"},
		},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, _ = sendKey(app, runeKey('l'))
	if app.postingCursor != 1 {
		t.Fatalf("postingCursor after 'l' = %d, want 1", app.postingCursor)
	}
	if app.screen != screenPostingDetail {
		t.Fatalf("screen after 'l' = %v, want screenPostingDetail (stay in detail)", app.screen)
	}

	app, _ = sendKey(app, runeKey('h'))
	if app.postingCursor != 0 {
		t.Fatalf("postingCursor after 'h' = %d, want 0", app.postingCursor)
	}
}

func TestApp_PostingDetail_LongLine_WrapsToViewportWidth(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	longLine := strings.Repeat("word ", 40)
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longLine}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 20, Height: 20})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	for _, line := range strings.Split(app.detailViewport.View(), "\n") {
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: longLine}},
	})
	app := newTestApp(t, s, syncer)
	// Tall viewport (200 rows) so every wrapped line is visible in View()
	// at once -- isolates the width re-wrap behavior from scroll/height.
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 200})
	app = openPostingList(t, app)
	app = openPostingDetail(app)

	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 15, Height: 200})

	view := app.detailViewport.View()
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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

	if diff := cmp.Diff([]string{"Engineering"}, app.filterDepartmentOptions); diff != "" {
		t.Fatalf("filterDepartmentOptions mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Remote"}, app.filterLocationOptions); diff != "" {
		t.Fatalf("filterLocationOptions mismatch (-want +got):\n%s", diff)
	}
}

func TestApp_FilterSelect_SpaceTogglesSelection(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering", Location: "Remote"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	app, cmd := sendKey(app, runeKey('f'))
	app, _ = sendKey(app, cmd())

	if app.filterSelectedDepartments["Engineering"] {
		t.Fatal("Engineering should not be selected initially")
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeySpace})
	if !app.filterSelectedDepartments["Engineering"] {
		t.Fatal("Engineering should be selected after space")
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeySpace})
	if app.filterSelectedDepartments["Engineering"] {
		t.Fatal("Engineering should be deselected after second space")
	}
}

func TestApp_FilterSelect_Esc_CancelsWithoutSaving(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	wantDept := app.filterDepartmentOptions[0]
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
	if derefOr(app.postings[0].Department, "") != wantDept {
		t.Fatalf("remaining posting department = %q, want %q", derefOr(app.postings[0].Department, ""), wantDept)
	}

	saved, err := s.ListCompanyFilters(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(saved) != 1 || saved[0].Field != "department" || saved[0].Value != wantDept {
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
	if derefOr(app.postings[0].Department, "") != wantDept {
		t.Fatalf("posting department after reload = %q, want %q", derefOr(app.postings[0].Department, ""), wantDept)
	}
}

func TestApp_CompanyRefreshed_ForCurrentlyViewedCompany_ReloadsPostings(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	if _, err := s.CreateCompanyFilter(context.Background(), acme.ID, "department", "Engineering"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	if derefOr(app.postings[0].Department, "") != "Engineering" {
		t.Fatalf("posting department = %q, want %q", derefOr(app.postings[0].Department, ""), "Engineering")
	}
}

func TestApp_OpenPostingList_TracksActiveFiltersFromExisting(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(context.Background(), acme.ID, "department", "Engineering"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	if _, err := s.CreateCompanyFilter(context.Background(), acme.ID, "location", "Remote"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
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
	syncer := newTestSyncer(s, map[string][]ashby.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", Department: "Engineering"}},
	})
	app := newTestApp(t, s, syncer)
	app = openPostingList(t, app)
	if len(app.activeFilterDepartments) != 0 {
		t.Fatalf("initial activeFilterDepartments = %+v, want empty", app.activeFilterDepartments)
	}

	app, cmd := sendKey(app, runeKey('f'))
	app, _ = sendKey(app, cmd())
	wantDept := app.filterDepartmentOptions[0]
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
