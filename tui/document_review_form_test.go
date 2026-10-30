package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

var testReviewDocument = message.DocumentReview{ApplicationID: 7, DocumentType: message.DocumentCoverLetter, Content: "Dear hiring manager"}

var testReviewSize = message.ContentSize{Width: 80, Rows: 10}

// reviewFormOn is a form with no services, opened on testReviewDocument.
func reviewFormOn() *documentReviewFormModel {
	return newDocumentReviewFormModel(nil, nil, testReviewDocument, testReviewSize)
}

// routed is a router with m registered and in focus.
func routed(t *testing.T, m model.Routable) *model.MsgRouter {
	t.Helper()
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Dispatch(focusOn(m.Instance()))
	return r
}

func TestDocumentReviewFormModel_OpensWithAFocusedEmptyTextarea(t *testing.T) {
	t.Parallel()

	m := reviewFormOn()
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("textarea.Value() = %q, want empty", got)
	}
	if !m.textarea.Focused() {
		t.Fatal("textarea should be focused on construction")
	}
}

func TestDocumentReviewFormModel_Esc_PopsAndClearsTheBanner(t *testing.T) {
	t.Parallel()

	m := reviewFormOn()
	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}))
	if !popsIn(got, m.Instance()) {
		t.Errorf("esc sent %#v, want a pop of the form", got)
	}
	if status, ok := statusIn(got); !ok || status != "" {
		t.Errorf("esc sent %#v, want an empty Notify", got)
	}
}

// ctrl+s and ctrl+g save the review, passed or flagged, of the content
// the form opened on, and say so about its application.
func TestDocumentReviewFormModel_SaveKeys_SaveTheReview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key     tea.KeyPressMsg
		outcome document.Outcome
	}{
		{key: tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, outcome: document.Passed},
		{key: tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}, outcome: document.Flagged},
	}
	for _, tt := range tests {
		t.Run(tt.key.String(), func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			stored := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
			docs := documents.NewStore(t.TempDir())
			if _, err := docs.Write(stored.ID, document.CoverLetter, "a draft"); err != nil {
				t.Fatalf("Write: %v", err)
			}
			m := newDocumentReviewFormModel(s.Documents, docs, message.DocumentReview{ApplicationID: stored.ID, DocumentType: message.DocumentCoverLetter, Content: "a draft"}, testReviewSize)
			m.keyPressed(runeKey([]rune("tighten it")...))

			got := runCmd(t, m.keyPressed(tt.key))

			if len(got) != 1 {
				t.Fatalf("%s sent %#v, want one Changed", tt.key, got)
			}
			changed, ok := got[0].(message.Event[message.Changed[message.Review]])
			if !ok {
				t.Fatalf("%s sent %T, want a Changed of the review", tt.key, got[0])
			}
			assertEventFrom(t, changed.Meta, message.Source{Instance: m.Instance()})
			if want := (message.Subject{Type: message.TypeApplication, ID: stored.ID}); changed.Meta.Subject != want || changed.Meta.Op != message.OpCreated {
				t.Errorf("Subject, Op = %+v, %v; want %+v, %v", changed.Meta.Subject, changed.Meta.Op, want, message.OpCreated)
			}
			after := changed.Payload.After
			if after.ApplicationID != stored.ID || after.DocumentType != message.DocumentCoverLetter || after.Outcome != outcomeSnapshots[tt.outcome] || after.Notes != "tighten it" {
				t.Errorf("After = %+v, want the cover letter's review, %v, with the notes typed", after, tt.outcome)
			}
			reviews, err := s.Documents.LatestReviews(context.Background(), stored.ID)
			if err != nil {
				t.Fatalf("LatestReviews: %v", err)
			}
			if review := reviews[document.CoverLetter]; review.ID != after.ID || review.ContentSnapshot != "a draft" {
				t.Errorf("stored review = %+v, want review %d of the content the form opened on", review, after.ID)
			}
		})
	}
}

// TestDocumentReviewFormModel_CtrlP_MovesTextareaCursorInsteadOfSaving
// pins the fix for a real bug: ctrl+p/ctrl+f were originally chosen as
// submit keys, but bubbles/textarea's own DefaultKeyMap already binds
// ctrl+p to "previous line" -- pressing it while editing multi-line
// notes must move the cursor, not silently submit the review. The
// textarea's own Update legitimately returns a non-nil cmd on most
// keystrokes (cursor-blink bookkeeping), so cmd-nil-ness can't
// distinguish "moved the cursor" from "submitted"; the only way to tell
// them apart is to run the returned cmd and check what message it
// produces. The form has no services here specifically so that if this
// test regressed (ctrl+p still saving), invoking that cmd would panic
// rather than silently passing.
func TestDocumentReviewFormModel_CtrlP_MovesTextareaCursorInsteadOfSaving(t *testing.T) {
	t.Parallel()

	m := reviewFormOn()
	m.keyPressed(runeKey([]rune("line one")...))
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.keyPressed(runeKey([]rune("line two")...))

	if cmd := m.keyPressed(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}); cmd != nil {
		if _, saved := cmd().(message.Event[message.Changed[message.Review]]); saved {
			t.Fatal("ctrl+p saved the review, want it to move the cursor instead")
		}
	}
	if got := m.textarea.Value(); got != "line one\nline two" {
		t.Fatalf("textarea.Value() after ctrl+p = %q, want notes untouched", got)
	}
}

func TestDocumentReviewFormModel_TypingKey_UpdatesTextareaValue(t *testing.T) {
	t.Parallel()

	m := reviewFormOn()
	m.keyPressed(runeKey([]rune("too generic")...))
	if got := m.textarea.Value(); got != "too generic" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "too generic")
	}
}

func TestDocumentReviewFormModel_TakesKeysOnlyWhileFocused(t *testing.T) {
	t.Parallel()

	m := reviewFormOn()
	r := routed(t, m)
	r.Dispatch(focusOn(reviewFormOn().Instance()))

	r.Dispatch(runeKey([]rune("ignored")...))

	if got := m.textarea.Value(); got != "" {
		t.Errorf("textarea.Value() = %q, want nothing typed while another screen is in focus", got)
	}
}

// reviewSaved is a review of testReviewDocument saved by instance.
func reviewSaved(instance message.ScreenInstance) tea.Msg {
	return message.New(message.Source{Instance: instance}, message.Subject{Type: message.TypeApplication, ID: testReviewDocument.ApplicationID}, message.OpCreated, time.Now().UTC(),
		message.Changed[message.Review]{After: message.Review{ID: 3, ApplicationID: testReviewDocument.ApplicationID, DocumentType: testReviewDocument.DocumentType, Outcome: message.ReviewPassed}})
}

// refusedBy is instance's save of testReviewDocument, refused with code.
func refusedBy(instance message.ScreenInstance, code message.ErrorCode) tea.Msg {
	return message.New(message.Source{Instance: instance}, message.Subject{Type: message.TypeApplication, ID: testReviewDocument.ApplicationID}, message.OpCreated, time.Now().UTC(),
		message.Error{Code: code, Text: "refused"})
}

// The form closes on its own save only. Its own failure leaves it open
// for another go, with the notes kept, and so does another form's save.
func TestDocumentReviewFormModel_ClosesOnItsOwnSaveOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		result    func(m *documentReviewFormModel) tea.Msg
		wantClose bool
	}{
		{name: "its own, saved", result: func(m *documentReviewFormModel) tea.Msg { return reviewSaved(m.Instance()) }, wantClose: true},
		{name: "its own, failed", result: func(m *documentReviewFormModel) tea.Msg {
			return message.New(message.Source{Instance: m.Instance()}, message.Subject{Type: message.TypeApplication, ID: testReviewDocument.ApplicationID},
				message.OpCreated, time.Now().UTC(), model.ErrorOf(errors.New("disk full")))
		}},
		{name: "another form's, saved", result: func(*documentReviewFormModel) tea.Msg { return reviewSaved(reviewFormOn().Instance()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := reviewFormOn()
			r := routed(t, m)

			got := runCmd(t, r.Dispatch(tt.result(m)))

			if closed := popsIn(got, m.Instance()); closed != tt.wantClose {
				t.Errorf("sent %#v, want a pop: %v", got, tt.wantClose)
			}
			if status, ok := statusIn(got); tt.wantClose && (!ok || status != "") {
				t.Errorf("sent %#v, want an empty Notify with the pop", got)
			}
		})
	}
}

// A save refused because the document changed switches the form to the
// document as it is now and shows what changed, so the next save reviews
// that. Another form's refusal leaves it alone.
func TestDocumentReviewFormModel_DocumentChanged_ReloadsWithTheDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		from       func(m *documentReviewFormModel) message.ScreenInstance
		wantReload bool
	}{
		{name: "its own", from: (*documentReviewFormModel).Instance, wantReload: true},
		{name: "another form's", from: func(*documentReviewFormModel) message.ScreenInstance { return reviewFormOn().Instance() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			docs := documents.NewStore(t.TempDir())
			if _, err := docs.Write(testReviewDocument.ApplicationID, document.CoverLetter, "Rewritten meanwhile\n"); err != nil {
				t.Fatalf("Write: %v", err)
			}
			opened := testReviewDocument
			opened.Content = "What you reviewed\n"
			m := newDocumentReviewFormModel(nil, docs, opened, testReviewSize)
			r := routed(t, m)
			m.keyPressed(runeKey([]rune("tighten the intro")...))

			got := runCmd(t, r.Dispatch(refusedBy(tt.from(m), message.ErrDocumentChanged)))

			if popsIn(got, m.Instance()) {
				t.Errorf("sent %#v, want the form kept open", got)
			}
			view := m.View().Content
			for _, want := range []string{"-What you reviewed", "+Rewritten meanwhile"} {
				if strings.Contains(view, want) != tt.wantReload {
					t.Errorf("view contains %q: %v, want %v:\n%s", want, !tt.wantReload, tt.wantReload, view)
				}
			}
			if !strings.Contains(view, "tighten the intro") {
				t.Errorf("view lost the notes typed:\n%s", view)
			}
		})
	}
}

func TestDocumentReviewFormModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, reviewFormOn())
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
	app = sendKeyAndApply(t, app, runeKey('r'))
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
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
	app = sendKeyAndApply(t, app, runeKey('r'))
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
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
	app = sendKeyAndApply(t, app, runeKey('r'))
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
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
	app = sendKeyAndApply(t, app, runeKey('r'))
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
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
	app = sendKeyAndApply(t, app, runeKey('r'))
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter}) // cover letter
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
