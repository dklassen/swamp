package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
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

// TestApp_ReviewOfADocumentThatChanged_ReloadsWithTheDiffInsteadOfSaving:
// an agent's write_document (or $EDITOR) can rewrite the document while
// the form is open. A review of the old version would never count as
// current and an agent would never see its notes, so the form refuses to
// save it: it keeps your notes, switches to the current content and shows
// what changed, and the next save reviews what's on disk (RFC 0007, H7).
func TestApp_ReviewOfADocumentThatChanged_ReloadsWithTheDiffInsteadOfSaving(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 40})
	app = openPostingList(t, app)
	application, err := s.CreateApplication(context.Background(), app.postings[0].ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	path, err := app.documents.Write(application.ID, documents.CoverLetter, "Dear team,\nWhat you reviewed\n")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter}) // cover letter
	app, _ = sendKey(app, runeKey([]rune("tighten the intro")...))
	if err := os.WriteFile(path, []byte("Dear team,\nRewritten meanwhile\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyCtrlS})

	if app.screen != screenDocumentReviewForm {
		t.Fatalf("screen after saving a changed document = %v, want still the review form", app.screen)
	}
	reviews, err := s.LatestDocumentReviews(context.Background(), application.ID)
	if err != nil {
		t.Fatalf("LatestDocumentReviews: %v", err)
	}
	if _, ok := reviews[documents.CoverLetter]; ok {
		t.Fatal("a review of the old version was saved, want none")
	}
	view := app.View()
	for _, want := range []string{"changed while you were reviewing", "-What you reviewed", "+Rewritten meanwhile", "tighten the intro"} {
		if !strings.Contains(view, want) {
			t.Errorf("view doesn't contain %q:\n%s", want, view)
		}
	}

	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyCtrlS})

	if app.screen == screenDocumentReviewForm {
		t.Fatal("second save left the form open, want the review saved")
	}
	reviews, err = s.LatestDocumentReviews(context.Background(), application.ID)
	if err != nil {
		t.Fatalf("LatestDocumentReviews: %v", err)
	}
	review := reviews[documents.CoverLetter]
	if review.ContentSnapshot != "Dear team,\nRewritten meanwhile\n" || review.Notes != "tighten the intro" {
		t.Errorf("review = snapshot %q, notes %q; want the current content and the notes typed before the reload", review.ContentSnapshot, review.Notes)
	}
}

func TestApp_ReviewOfADocumentDeletedMeanwhile_SavesNothingAndSaysSo(t *testing.T) {
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 40})
	app = openPostingList(t, app)
	application, err := s.CreateApplication(context.Background(), app.postings[0].ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	path, err := app.documents.Write(application.ID, documents.CoverLetter, "a draft")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter}) // cover letter
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyCtrlS})

	if app.err == nil || !strings.Contains(app.err.Error(), "deleted while you were reviewing") {
		t.Errorf("err = %v, want one saying the document was deleted", app.err)
	}
	reviews, err := s.LatestDocumentReviews(context.Background(), application.ID)
	if err != nil {
		t.Fatalf("LatestDocumentReviews: %v", err)
	}
	if _, ok := reviews[documents.CoverLetter]; ok {
		t.Error("a review was saved for a deleted document, want none")
	}
}

// TestApp_ReviewOfADocumentTheAgentRewrote_SaysWhoAndWhen: when Swamp
// recorded the write that produced the current version (#258), the
// notice names who made it and when, in local time.
func TestApp_ReviewOfADocumentTheAgentRewrote_SaysWhoAndWhen(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 40})
	app = openPostingList(t, app)
	application, err := s.CreateApplication(ctx, app.postings[0].ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := app.documents.Write(application.ID, documents.CoverLetter, "What you reviewed\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter}) // cover letter
	if _, err := app.documents.Write(application.ID, documents.CoverLetter, "Rewritten by the agent\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.RecordDocumentWrite(ctx, application.ID, documents.CoverLetter, "Rewritten by the agent\n", store.DocumentWriteSourceWriteDocument); err != nil {
		t.Fatalf("RecordDocumentWrite: %v", err)
	}
	write, _, err := s.LatestDocumentWrite(ctx, application.ID, documents.CoverLetter)
	if err != nil {
		t.Fatalf("LatestDocumentWrite: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyCtrlS})

	view := app.View()
	for _, want := range []string{"rewritten by the agent", write.WrittenAt.Local().Format("15:04")} {
		if !strings.Contains(view, want) {
			t.Errorf("view doesn't contain %q:\n%s", want, view)
		}
	}
}

// TestApp_ReviewOfADocumentChangedOutsideSwamp_DoesNotNameTheLastWriter:
// the agent wrote the version you're reviewing, then something outside
// Swamp changed it. The latest recorded write isn't of the current
// version, so the notice mustn't credit the agent with the change.
func TestApp_ReviewOfADocumentChangedOutsideSwamp_DoesNotNameTheLastWriter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 40})
	app = openPostingList(t, app)
	application, err := s.CreateApplication(ctx, app.postings[0].ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	path, err := app.documents.Write(application.ID, documents.CoverLetter, "The agent's draft\n")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.RecordDocumentWrite(ctx, application.ID, documents.CoverLetter, "The agent's draft\n", store.DocumentWriteSourceWriteDocument); err != nil {
		t.Fatalf("RecordDocumentWrite: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter}) // cover letter
	if err := os.WriteFile(path, []byte("Changed outside Swamp\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyCtrlS})

	view := app.View()
	if !strings.Contains(view, "changed while you were reviewing") {
		t.Fatalf("view doesn't show the changed notice:\n%s", view)
	}
	if strings.Contains(view, "by the agent") {
		t.Errorf("notice credits the agent with a change made outside Swamp:\n%s", view)
	}
}
