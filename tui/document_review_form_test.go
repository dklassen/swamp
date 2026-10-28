package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

func TestDocumentReviewFormModel_New_SeedsFocusedEmptyTextarea(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, document.CoverLetter, "Dear hiring manager", 80, 10)
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("textarea.Value() = %q, want empty", got)
	}
	if !m.textarea.Focused() {
		t.Fatal("textarea should be focused on construction")
	}
}

func TestDocumentReviewFormModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, document.CoverLetter, "content", 80, 10)
	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelDocumentReviewFormMsg); !ok {
		t.Fatalf("intent = %T, want cancelDocumentReviewFormMsg", intent)
	}
}

func TestDocumentReviewFormModel_CtrlS_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, document.CoverLetter, "content", 80, 10)
	cmd, intent := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the review as passed")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestDocumentReviewFormModel_CtrlG_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, nil, 1, document.CoverLetter, "content", 80, 10)
	cmd, intent := m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
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

	m := newDocumentReviewFormModel(nil, nil, 1, document.CoverLetter, "content", 80, 10)
	m.Update(runeKey([]rune("line one")...))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(runeKey([]rune("line two")...))

	cmd, intent := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
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

	m := newDocumentReviewFormModel(nil, nil, 1, document.CoverLetter, "content", 80, 10)
	m.Update(runeKey([]rune("too generic")...))
	if got := m.textarea.Value(); got != "too generic" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "too generic")
	}
}

func TestApp_ReviewSavedOfAnUnchangedDocument_DoesNotWarn(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 20})
	app = openPostingList(t, app)
	application, err := createApplicationIn(context.Background(), s, openList(t, app).list.Postings[0].ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	if _, err := app.documents.Write(application.ID, document.CoverLetter, "# What you reviewed"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	if view := viewText(app); strings.Contains(view, "while you were reviewing") {
		t.Errorf("view warns about a document that didn't change:\n%s", view)
	}
}

// TestApp_ReviewOfADocumentThatChanged_ReloadsWithTheDiffInsteadOfSaving:
// an agent's write_document (or $EDITOR) can rewrite the document while
// the form is open. A review of the old version would never count as
// current and an agent would never see its notes, so the form refuses to
// save it: it keeps your notes, switches to the current content and shows
// what changed, and the next save reviews what's on disk.
func TestApp_ReviewOfADocumentThatChanged_ReloadsWithTheDiffInsteadOfSaving(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 40})
	app = openPostingList(t, app)
	application, err := createApplicationIn(context.Background(), s, openList(t, app).list.Postings[0].ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	path, err := app.documents.Write(application.ID, document.CoverLetter, "Dear team,\nWhat you reviewed\n")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
	app, _ = sendKey(app, runeKey([]rune("tighten the intro")...))
	if err := os.WriteFile(path, []byte("Dear team,\nRewritten meanwhile\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	if app.currentScreen != screenDocumentReviewForm {
		t.Fatalf("screen after saving a changed document = %v, want still the review form", app.currentScreen)
	}
	reviews, err := s.Documents.LatestReviews(context.Background(), application.ID)
	if err != nil {
		t.Fatalf("LatestReviews: %v", err)
	}
	if _, ok := reviews[document.CoverLetter]; ok {
		t.Fatal("a review of the old version was saved, want none")
	}
	view := viewText(app)
	for _, want := range []string{"changed while you were reviewing", "-What you reviewed", "+Rewritten meanwhile", "tighten the intro"} {
		if !strings.Contains(view, want) {
			t.Errorf("view doesn't contain %q:\n%s", want, view)
		}
	}

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	if app.currentScreen == screenDocumentReviewForm {
		t.Fatal("second save left the form open, want the review saved")
	}
	reviews, err = s.Documents.LatestReviews(context.Background(), application.ID)
	if err != nil {
		t.Fatalf("LatestReviews: %v", err)
	}
	review := reviews[document.CoverLetter]
	if review.ContentSnapshot != "Dear team,\nRewritten meanwhile\n" || review.Notes != "tighten the intro" {
		t.Errorf("review = snapshot %q, notes %q; want the current content and the notes typed before the reload", review.ContentSnapshot, review.Notes)
	}
}

func TestApp_ReviewOfADocumentDeletedMeanwhile_SavesNothingAndSaysSo(t *testing.T) {
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 40})
	app = openPostingList(t, app)
	application, err := createApplicationIn(context.Background(), s, openList(t, app).list.Postings[0].ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	path, err := app.documents.Write(application.ID, document.CoverLetter, "a draft")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	if app.err == nil || !strings.Contains(app.err.Error(), "deleted while you were reviewing") {
		t.Errorf("err = %v, want one saying the document was deleted", app.err)
	}
	reviews, err := s.Documents.LatestReviews(context.Background(), application.ID)
	if err != nil {
		t.Fatalf("LatestReviews: %v", err)
	}
	if _, ok := reviews[document.CoverLetter]; ok {
		t.Error("a review was saved for a deleted document, want none")
	}
}

// TestApp_ReviewOfADocumentTheAgentRewrote_SaysWhoAndWhen: when Swamp
// recorded the write that produced the current version, the
// notice names who made it and when, in local time.
func TestApp_ReviewOfADocumentTheAgentRewrote_SaysWhoAndWhen(t *testing.T) {
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 40})
	app = openPostingList(t, app)
	application, err := createApplicationIn(ctx, s, openList(t, app).list.Postings[0].ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	if _, err := app.documents.Write(application.ID, document.CoverLetter, "What you reviewed\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
	if _, err := app.documents.Write(application.ID, document.CoverLetter, "Rewritten by the agent\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.Documents.RecordWrite(ctx, application.ID, document.CoverLetter, "Rewritten by the agent\n", document.SourceWriteDocument); err != nil {
		t.Fatalf("RecordWrite: %v", err)
	}
	write, err := s.Documents.LatestWrite(ctx, application.ID, document.CoverLetter)
	if err != nil {
		t.Fatalf("LatestWrite: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	view := viewText(app)
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
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}},
	})
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 300, Height: 40})
	app = openPostingList(t, app)
	application, err := createApplicationIn(ctx, s, openList(t, app).list.Postings[0].ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	path, err := app.documents.Write(application.ID, document.CoverLetter, "The agent's draft\n")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.Documents.RecordWrite(ctx, application.ID, document.CoverLetter, "The agent's draft\n", document.SourceWriteDocument); err != nil {
		t.Fatalf("RecordWrite: %v", err)
	}

	app = openPostingDetail(t, app)
	app, _ = sendKey(app, runeKey('r'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
	if err := os.WriteFile(path, []byte("Changed outside Swamp\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	view := viewText(app)
	if !strings.Contains(view, "changed while you were reviewing") {
		t.Fatalf("view doesn't show the changed notice:\n%s", view)
	}
	if strings.Contains(view, "by the agent") {
		t.Errorf("notice credits the agent with a change made outside Swamp:\n%s", view)
	}
}
