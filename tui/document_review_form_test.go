package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/jobboard"
)

func TestDocumentReviewFormModel_New_SeedsFocusedEmptyTextarea(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, documents.CoverLetter, "Dear hiring manager", 80, 10, 0)
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("textarea.Value() = %q, want empty", got)
	}
	if !m.textarea.Focused() {
		t.Fatal("textarea should be focused on construction")
	}
}

func TestDocumentReviewFormModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, documents.CoverLetter, "content", 80, 10, 0)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelDocumentReviewFormMsg); !ok {
		t.Fatalf("intent = %T, want cancelDocumentReviewFormMsg", intent)
	}
}

func TestDocumentReviewFormModel_CtrlS_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, documents.CoverLetter, "content", 80, 10, 0)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the review as passed")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestDocumentReviewFormModel_CtrlG_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, documents.CoverLetter, "content", 80, 10, 0)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the review as flagged")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

// TestDocumentReviewFormModel_CtrlP_MovesTextareaCursorInsteadOfSaving
// pins the fix for a real bug: ctrl+p/ctrl+f were originally chosen as
// submit keys, but bubbles/textarea's own DefaultKeyMap already binds
// ctrl+p to "previous line" -- pressing it while editing multi-line
// notes must move the cursor, not silently submit the review. The
// textarea's own Update legitimately returns a non-nil cmd on most
// keystrokes (cursor-blink bookkeeping), so cmd-nil-ness can't
// distinguish "moved the cursor" from "submitted" -- both the submit
// path and the textarea's default path return a nil intent too, so the
// only way to actually tell them apart is to run the returned cmd and
// check what message it produces. m.store is nil here specifically so
// that if this test regressed (ctrl+p still routed to
// createDocumentReview), invoking that cmd would panic on the nil
// store rather than silently passing.
func TestDocumentReviewFormModel_CtrlP_MovesTextareaCursorInsteadOfSaving(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, documents.CoverLetter, "content", 80, 10, 0)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line one")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line two")})

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
	if cmd != nil {
		if _, isReviewCreated := cmd().(documentReviewCreatedMsg); isReviewCreated {
			t.Fatal("ctrl+p triggered createDocumentReview, want it to move the cursor instead")
		}
	}
	if got := m.textarea.Value(); got != "line one\nline two" {
		t.Fatalf("textarea.Value() after ctrl+p = %q, want notes untouched", got)
	}
}

func TestDocumentReviewFormModel_TypingKey_UpdatesTextareaValue(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, documents.CoverLetter, "content", 80, 10, 0)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("too generic")})
	if got := m.textarea.Value(); got != "too generic" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "too generic")
	}
}

// TestApp_ReviewSavedAfterTheDocumentChanged_WarnsAndKeepsTheSnapshot: an
// agent's write_document (or $EDITOR) can change the document while the
// form is open. The review still records what you saw, but it won't count
// as current, so the TUI says so instead of letting it vanish silently
// (RFC 0007, H7).
func TestApp_ReviewSavedAfterTheDocumentChanged_WarnsAndKeepsTheSnapshot(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)
	application, err := s.CreateApplication(context.Background(), app.postings[0].ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	path, err := app.documents.Write(application.ID, documents.CoverLetter, "# What you reviewed")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter}) // cover letter
	if app.screen != screenDocumentReviewForm {
		t.Fatalf("screen = %v, want the review form", app.screen)
	}
	if err := os.WriteFile(path, []byte("# Rewritten meanwhile"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyCtrlS})

	if view := app.View(); !strings.Contains(view, "changed while you were reviewing") {
		t.Errorf("view doesn't warn that the document changed:\n%s", view)
	}
	reviews, err := s.LatestDocumentReviews(context.Background(), application.ID)
	if err != nil {
		t.Fatalf("LatestDocumentReviews: %v", err)
	}
	if got := reviews[documents.CoverLetter].ContentSnapshot; got != "# What you reviewed" {
		t.Errorf("review snapshot = %q, want what the form showed", got)
	}
}

func TestApp_ReviewSavedOfAnUnchangedDocument_DoesNotWarn(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)
	application, err := s.CreateApplication(context.Background(), app.postings[0].ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := app.documents.Write(application.ID, documents.CoverLetter, "# What you reviewed"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter}) // cover letter
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyCtrlS})

	if view := app.View(); strings.Contains(view, "while you were reviewing") {
		t.Errorf("view warns about a document that didn't change:\n%s", view)
	}
}
