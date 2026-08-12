package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

// newTestApp creates an App and drives it through Init, returning the
// model with its initial companies already loaded.
func newTestApp(t *testing.T, s *store.Store) *App {
	t.Helper()
	app := New(s)
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

func TestApp_Init_LoadsCompanies(t *testing.T) {
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	app := newTestApp(t, s)

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
	app := newTestApp(t, s)

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
	app := newTestApp(t, s)

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
	app := newTestApp(t, s)

	app, _ = sendKey(app, runeKey('a'))
	app, _ = sendKey(app, runeKey('A', 'c', 'm', 'e'))

	if got := app.formInputs[0].Value(); got != "Acme" {
		t.Fatalf("formInputs[0].Value() = %q, want %q", got, "Acme")
	}
}

func TestApp_Tab_MovesFocusToNextField(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s)

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
	app := newTestApp(t, s)

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
	app := newTestApp(t, s)

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
	app := newTestApp(t, s)

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
	app := newTestApp(t, s)

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
