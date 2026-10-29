package tui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

var testNotesApplication = message.Application{ID: 7, PostingID: 5, Status: message.ApplicationStarted, Notes: "notes"}

// notesEditorOn is an editor opened on testNotesApplication with notes.
func notesEditorOn(applications *service.ApplicationService, notes string) *applicationNotesModel {
	app := testNotesApplication
	app.Notes = notes
	return newApplicationNotesModel(applications, app, message.ContentSize{Width: 80, Rows: 10})
}

func TestApplicationNotesModel_OpensOnTheNavsNotes(t *testing.T) {
	t.Parallel()

	m := notesEditorOn(nil, "existing notes")
	if got := m.textarea.Value(); got != "existing notes" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "existing notes")
	}
	if !m.textarea.Focused() {
		t.Fatal("textarea should be focused on construction")
	}
}

func TestApplicationNotesModel_Esc_PopsAndClearsTheBanner(t *testing.T) {
	t.Parallel()

	m := notesEditorOn(nil, "")
	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}))
	if !popsIn(got, m.Instance()) {
		t.Errorf("esc sent %#v, want a pop of the editor", got)
	}
	if status, ok := statusIn(got); !ok || status != "" {
		t.Errorf("esc sent %#v, want an empty Notify", got)
	}
}

func TestApplicationNotesModel_TypingKey_UpdatesTextareaValue(t *testing.T) {
	t.Parallel()

	m := notesEditorOn(nil, "")
	m.keyPressed(runeKey([]rune("hi")...))
	if got := m.textarea.Value(); got != "hi" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "hi")
	}
}

func TestApplicationNotesModel_EscWithUnsavedEdits_AsksBeforeDiscarding(t *testing.T) {
	t.Parallel()

	m := notesEditorOn(nil, "notes")
	m.textarea.InsertString(" and more")
	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}); cmd != nil {
		t.Fatalf("esc with unsaved edits sent a command; want none (screen stays to ask)")
	}
	if !m.confirmingDiscard {
		t.Error("esc with unsaved edits didn't open the discard question")
	}
}

func TestApplicationNotesModel_DiscardPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		key        tea.KeyPressMsg
		wantPop    bool
		wantSave   bool
		wantPrompt bool
	}{
		{name: "y discards", key: runeKey('y'), wantPop: true},
		{name: "n keeps editing", key: runeKey('n')},
		{name: "esc keeps editing", key: tea.KeyPressMsg{Code: tea.KeyEsc}},
		{name: "ctrl+s saves", key: tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, wantSave: true},
		{name: "other keys are ignored", key: runeKey('x'), wantPrompt: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// A service on no database fails every write.
			m := notesEditorOn(service.NewApplicationService(store.Store{}), "notes")
			m.textarea.InsertString(" and more")
			typed := m.textarea.Value()
			m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc})

			got := runCmd(t, m.keyPressed(tt.key))
			if popped := popsIn(got, m.Instance()); popped != tt.wantPop {
				t.Errorf("sent %#v, want a pop: %v", got, tt.wantPop)
			}
			if saved := m.saving; saved != tt.wantSave {
				t.Errorf("saving = %v, want %v", saved, tt.wantSave)
			}
			if prompt := m.confirmingDiscard; !tt.wantPop && prompt != tt.wantPrompt {
				t.Errorf("prompt shown: %v, want %v", prompt, tt.wantPrompt)
			}
			if got := m.textarea.Value(); got != typed {
				t.Errorf("textarea = %q, want %q unchanged", got, typed)
			}
		})
	}
}

// TestApplicationNotesModel_WhileSaving_IgnoresKeys: once ctrl+s is
// pressed, the screen waits for the save's result, so esc can't leave
// with the save's outcome unseen and typing can't change what's saved.
func TestApplicationNotesModel_WhileSaving_IgnoresKeys(t *testing.T) {
	t.Parallel()

	m := notesEditorOn(nil, "notes")
	m.textarea.InsertString(" and more")
	m.keyPressed(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('x'), {Code: 's', Mod: tea.ModCtrl}} {
		if cmd := m.keyPressed(key); cmd != nil {
			t.Errorf("%v while saving sent a command; want none", key)
		}
	}
	if got := m.textarea.Value(); got != "notes and more" {
		t.Errorf("textarea = %q, want %q", got, "notes and more")
	}
}

// The editor closes on its own save only. Its own failure lets editing go
// on, and a write from anywhere else leaves it waiting for its own.
func TestApplicationNotesModel_ActsOnItsOwnResultOnly(t *testing.T) {
	t.Parallel()

	before := testNotesApplication
	after := before
	after.Notes = "notes and more"
	saved := func(instance message.ScreenInstance) tea.Msg {
		return message.Updated(message.Source{Instance: instance}, before.ID, time.Now().UTC(), before, after)
	}
	tests := []struct {
		name       string
		result     func(m *applicationNotesModel) tea.Msg
		wantClose  bool
		wantSaving bool
	}{
		{name: "its own, saved", result: func(m *applicationNotesModel) tea.Msg { return saved(m.Instance()) }, wantClose: true, wantSaving: true},
		{name: "its own, failed", result: func(m *applicationNotesModel) tea.Msg {
			return message.New(message.Source{Instance: m.Instance()}, message.Subject{Type: message.TypeApplication, ID: before.ID},
				message.OpUpdated, time.Now().UTC(), model.ErrorOf(errors.New("disk full")))
		}},
		{name: "another screen's", result: func(*applicationNotesModel) tea.Msg {
			return saved(notesEditorOn(nil, "").Instance())
		}, wantSaving: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := notesEditorOn(nil, "notes")
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}
			if cmd := m.keyPressed(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); cmd == nil {
				t.Fatal("ctrl+s: cmd = nil, want the save")
			}

			got := runCmd(t, r.Dispatch(tt.result(m)))

			if closed := popsIn(got, m.Instance()); closed != tt.wantClose {
				t.Errorf("sent %#v, want a pop: %v", got, tt.wantClose)
			}
			if status, ok := statusIn(got); tt.wantClose && (!ok || status != "") {
				t.Errorf("sent %#v, want an empty Notify with the pop", got)
			}
			if m.saving != tt.wantSaving {
				t.Errorf("saving = %v, want %v", m.saving, tt.wantSaving)
			}
		})
	}
}

func TestApplicationNotesModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, notesEditorOn(nil, ""))
}
