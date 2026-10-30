package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

var testFormApplication = message.Application{ID: 7, PostingID: 5, Status: message.ApplicationStarted}

// formOn is a form opened on testFormApplication whose stored form has
// been read as stored.
func formOn(postings *service.PostingService, stored message.ApplicationForm) *applicationFormModel {
	m := newApplicationFormModel(postings, testFormApplication, message.ContentSize{Width: 80, Rows: 20})
	m.formRead(formReadBy(m.Instance(), stored))
	return m
}

func formReadBy(instance message.ScreenInstance, form message.ApplicationForm) message.Event[message.Read[message.ApplicationForm]] {
	return message.New(message.Source{Instance: instance}, message.Subject{Type: message.TypePosting, ID: testFormApplication.PostingID},
		message.OpNone, time.Now().UTC(), message.Read[message.ApplicationForm]{Value: form})
}

func formFailedFor(instance message.ScreenInstance, op message.Op) tea.Msg {
	return message.New(message.Source{Instance: instance}, message.Subject{Type: message.TypePosting, ID: testFormApplication.PostingID},
		op, time.Now().UTC(), model.ErrorOf(errors.New("disk full")))
}

func TestApplicationFormModel_OpensOnTheStoredForm(t *testing.T) {
	t.Parallel()

	m := formOn(nil, message.ApplicationForm{
		Documents: map[message.DocumentType]message.FormRequirement{message.DocumentResume: message.FormRequired},
		Questions: []message.FormQuestion{{Label: "Why us?", Required: true}},
	})

	for _, want := range []string{"resume: required", "cover_letter: absent", "Why us? *"} {
		if got := m.textarea.Value(); !strings.Contains(got, want) {
			t.Errorf("textarea = %q, want it to have %q", got, want)
		}
	}
}

// Until the stored form is read there's nothing to edit, so typing waits;
// esc still leaves.
func TestApplicationFormModel_BeforeTheFormIsRead_OnlyEscWorks(t *testing.T) {
	t.Parallel()

	m := newApplicationFormModel(nil, testFormApplication, message.ContentSize{Width: 80, Rows: 20})
	m.keyPressed(runeKey('x'))
	m.paste(tea.PasteMsg{Content: "pasted"})
	if got := m.textarea.Value(); got != "" {
		t.Errorf("textarea = %q before the read, want it empty", got)
	}

	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}))
	if !popsIn(got, m.Instance()) {
		t.Errorf("esc sent %#v, want a pop of the form", got)
	}
}

// A form that can't be read closes, leaving why on the banner.
func TestApplicationFormModel_ReadFails_ClosesKeepingTheError(t *testing.T) {
	t.Parallel()

	m := newApplicationFormModel(nil, testFormApplication, message.ContentSize{Width: 80, Rows: 20})
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := runCmd(t, r.Dispatch(formFailedFor(m.Instance(), message.OpNone)))

	if !popsIn(got, m.Instance()) {
		t.Errorf("sent %#v, want a pop of the form", got)
	}
	if _, ok := statusIn(got); ok {
		t.Errorf("sent %#v, want no Notify, which would clear the error", got)
	}
}

// TestApplicationFormModel_InvalidText_StaysOpenAndSaysWhy: a line the
// form can't read isn't saved; the screen stays open and names the line,
// so the user can fix it without losing what they typed (#184).
func TestApplicationFormModel_InvalidText_StaysOpenAndSaysWhy(t *testing.T) {
	t.Parallel()

	m := formOn(nil, message.ApplicationForm{})
	m.textarea.SetValue("resume: maybe\nWhy us? *\n")
	if cmd := m.keyPressed(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("ctrl+s on invalid text sent a command; want none (nothing saved, screen stays)")
	}
	if view := m.View().Content; !strings.Contains(view, "line 1") || !strings.Contains(view, "maybe") {
		t.Errorf("view doesn't say which line is wrong:\n%s", view)
	}
	if got := m.textarea.Value(); !strings.Contains(got, "Why us? *") {
		t.Errorf("textarea lost what was typed: %q", got)
	}
}

// TestApplicationFormModel_EscWithUnsavedEdits_AsksBeforeDiscarding: esc
// doesn't throw away pasted questions without asking.
func TestApplicationFormModel_EscWithUnsavedEdits_AsksBeforeDiscarding(t *testing.T) {
	t.Parallel()

	m := formOn(nil, message.ApplicationForm{})
	m.textarea.InsertString("Why us? *\n")
	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}); cmd != nil {
		t.Fatalf("esc with unsaved edits sent a command; want none (screen stays to ask)")
	}
	if !m.confirmingDiscard {
		t.Error("esc with unsaved edits didn't open the discard question")
	}
}

// TestApplicationFormModel_DiscardPrompt covers each answer to the prompt
// esc raises over unsaved edits.
func TestApplicationFormModel_DiscardPrompt(t *testing.T) {
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
			m := formOn(service.NewPostingService(store.Store{}), message.ApplicationForm{})
			m.textarea.InsertString("Why us? *\n")
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

func TestApplicationFormModel_EscWithoutEdits_PopsAndClearsTheBanner(t *testing.T) {
	t.Parallel()

	m := formOn(nil, message.ApplicationForm{})
	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}))
	if !popsIn(got, m.Instance()) {
		t.Errorf("esc sent %#v, want a pop of the form", got)
	}
	if status, ok := statusIn(got); !ok || status != "" {
		t.Errorf("esc sent %#v, want an empty Notify", got)
	}
}

// TestApplicationFormModel_WhileSaving_IgnoresKeys: once a valid form is
// sent to be saved, the screen waits for the result.
func TestApplicationFormModel_WhileSaving_IgnoresKeys(t *testing.T) {
	t.Parallel()

	m := formOn(nil, message.ApplicationForm{})
	m.textarea.InsertString("Why us? *\n")
	if cmd := m.keyPressed(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+s on a valid form sent no save")
	}
	typed := m.textarea.Value()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('x'), {Code: 's', Mod: tea.ModCtrl}} {
		if cmd := m.keyPressed(key); cmd != nil {
			t.Errorf("%v while saving sent a command; want none", key)
		}
	}
	if got := m.textarea.Value(); got != typed {
		t.Errorf("textarea = %q, want %q", got, typed)
	}
}

func TestApplicationFormModel_PasteAtDiscardPrompt_IsIgnored(t *testing.T) {
	t.Parallel()

	m := formOn(nil, message.ApplicationForm{})
	m.textarea.InsertString("Why us? *\n")
	typed := m.textarea.Value()
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc})

	m.paste(tea.PasteMsg{Content: "y"})

	if got := m.textarea.Value(); got != typed {
		t.Errorf("text = %q, want %q unchanged", got, typed)
	}
	if !m.confirmingDiscard {
		t.Error("a paste answered the discard prompt")
	}
}

// The form closes on its own save only, saying so. Its own failure lets
// editing go on, and another screen's save leaves it waiting for its own.
func TestApplicationFormModel_ActsOnItsOwnResultOnly(t *testing.T) {
	t.Parallel()

	saved := func(instance message.ScreenInstance) tea.Msg {
		return message.New(message.Source{Instance: instance}, message.Subject{Type: message.TypePosting, ID: testFormApplication.PostingID},
			message.OpNone, time.Now().UTC(), message.Ran[message.ApplicationForm]{})
	}
	tests := []struct {
		name       string
		result     func(m *applicationFormModel) tea.Msg
		wantClose  bool
		wantSaving bool
	}{
		{name: "its own, saved", result: func(m *applicationFormModel) tea.Msg { return saved(m.Instance()) }, wantClose: true, wantSaving: true},
		{name: "its own, failed", result: func(m *applicationFormModel) tea.Msg { return formFailedFor(m.Instance(), message.OpUpdated) }},
		{name: "another screen's", result: func(*applicationFormModel) tea.Msg {
			return saved(formOn(nil, message.ApplicationForm{}).Instance())
		}, wantSaving: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := formOn(nil, message.ApplicationForm{})
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}
			m.textarea.InsertString("Why us? *\n")
			if cmd := m.keyPressed(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); cmd == nil {
				t.Fatal("ctrl+s: cmd = nil, want the save")
			}

			got := runCmd(t, r.Dispatch(tt.result(m)))

			if closed := popsIn(got, m.Instance()); closed != tt.wantClose {
				t.Errorf("sent %#v, want a pop: %v", got, tt.wantClose)
			}
			if status, ok := statusIn(got); tt.wantClose && (!ok || status != "Application form saved") {
				t.Errorf("sent %#v, want a Notify saying it saved with the pop", got)
			}
			if m.saving != tt.wantSaving {
				t.Errorf("saving = %v, want %v", m.saving, tt.wantSaving)
			}
		})
	}
}

func TestApplicationFormModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, formOn(nil, message.ApplicationForm{}))
}
