package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

func TestApp_PressP_TogglesPauseOnSelectedCompany(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))

	app, cmd := sendKey(app, runeKey('p'))
	if cmd == nil {
		t.Fatal("Update on 'p' returned nil Cmd, want a command that pauses the company")
	}
	app, _ = sendKey(app, cmd())

	if len(app.companies) != 0 {
		t.Fatalf("app.companies after pause = %+v, want empty (paused = soft-deleted, excluded from active list)", app.companies)
	}

	_, err := s.GetCompany(context.Background(), acme.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetCompany after pause error = %v, want ErrNotFound (soft-deleted companies are excluded)", err)
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
