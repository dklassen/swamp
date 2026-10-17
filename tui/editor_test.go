package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

func TestEditorCommand_EditorSet_ReturnsEditorAndPath(t *testing.T) {
	cmd, args, err := editorCommand("vim", "/tmp/cover_letter.md")
	if err != nil {
		t.Fatalf("editorCommand: %v", err)
	}
	if cmd != "vim" {
		t.Fatalf("cmd = %q, want %q", cmd, "vim")
	}
	if len(args) != 1 || args[0] != "/tmp/cover_letter.md" {
		t.Fatalf("args = %v, want [/tmp/cover_letter.md]", args)
	}
}

func TestEditorCommand_EditorUnset_ReturnsError(t *testing.T) {
	_, _, err := editorCommand("", "/tmp/cover_letter.md")
	if err == nil {
		t.Fatal("editorCommand: expected error when $EDITOR is unset, got nil")
	}
}

// TestApp_EditorClosed_ReloadsTheApplicationsReviews: editing a draft from
// application detail can make its latest review stale. When the editor
// closes, the badge must reflect the file as it is now, not as it was
// before the edit.
func TestApp_EditorClosed_ReloadsTheApplicationsReviews(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	target := mustCreateApplication(t, s, posting.ID)
	docs := documents.NewStore(t.TempDir())
	if _, err := docs.Write(target.ID, document.Resume, "# Draft\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := createReview(context.Background(), s, target.ID, document.Resume, "# Draft\n", document.Passed, ""); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	app := newAppOn(s, newTestSyncer(s, nil), docs)
	app = applyCmd(t, app, app.Init())
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if view := app.View(); !strings.Contains(view, "[PASSED]") {
		t.Fatalf("application detail before the edit doesn't show [PASSED]:\n%s", view)
	}

	if _, err := docs.Write(target.ID, document.Resume, "# Edited in $EDITOR\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	app = sendKeyAndApply(t, app, editorClosedMsg{applicationID: target.ID, documentType: document.Resume, before: documents.ContentSHA256("# Draft\n")})

	if view := app.View(); strings.Contains(view, "[PASSED]") {
		t.Errorf("application detail still shows the review of the old version as [PASSED]:\n%s", view)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	if app.screen != screenActiveApplications {
		t.Fatalf("screen after esc = %v, want the home list", app.screen)
	}
	if view := app.View(); strings.Contains(view, "R:✓") {
		t.Errorf("home list row still shows the resume as passed:\n%s", view)
	}
}

func TestApp_EditorClosedWithAnError_ShowsIt(t *testing.T) {
	t.Parallel()

	app, _ := deleteTestApp(t)
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})

	app = sendKeyAndApply(t, app, editorClosedMsg{err: errors.New("tui: $EDITOR is not set")})

	if app.err == nil || !strings.Contains(app.err.Error(), "$EDITOR is not set") {
		t.Errorf("err = %v, want the editor's error", app.err)
	}
}

// TestApp_EditorClosedAfterAChange_RecordsTheWrite: an edit in $EDITOR is
// recorded with source editor, so the change probe sees it and the review
// form can say you made it. Swamp can't see inside the editor, so
// it compares the file with the version from before the editor opened.
func TestApp_EditorClosedAfterAChange_RecordsTheWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app, target := deleteTestApp(t) // resume drafted as "# Draft\n"
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if _, err := app.documents.Write(target.ID, document.Resume, "# Edited\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	app = sendKeyAndApply(t, app, editorClosedMsg{applicationID: target.ID, documentType: document.Resume, before: documents.ContentSHA256("# Draft\n")})

	write, err := storeOf(app).Documents.LatestWrite(ctx, target.ID, document.Resume)
	if err != nil {
		t.Fatalf("LatestWrite: %v; want the edit recorded", err)
	}
	if write.Source != document.SourceEditor || write.ContentSHA256 != documents.ContentSHA256("# Edited\n") {
		t.Errorf("recorded write = source %v, hash %q; want editor and the edited content's hash", write.Source, write.ContentSHA256)
	}
}

func TestApp_EditorClosedWithoutAChange_RecordsNothing(t *testing.T) {
	t.Parallel()

	app, target := deleteTestApp(t) // resume drafted as "# Draft\n"
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})

	app = sendKeyAndApply(t, app, editorClosedMsg{applicationID: target.ID, documentType: document.Resume, before: documents.ContentSHA256("# Draft\n")})

	if _, err := storeOf(app).Documents.LatestWrite(context.Background(), target.ID, document.Resume); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("LatestWrite: err = %v, want store.ErrNotFound: nothing recorded for an unchanged document", err)
	}
}
