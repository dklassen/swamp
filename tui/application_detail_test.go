package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

func testApplicationView() store.ApplicationView {
	return store.ApplicationView{
		Application: store.Application{ID: 1, Status: store.ApplicationStatusStarted},
		Posting:     store.Posting{ID: 5, IngestedFields: store.IngestedFields{Title: "Engineer"}},
		CompanyName: "Acme",
	}
}

func TestApplicationDetailModel_EscOrB_ReturnsBackToActiveApplicationsMsg(t *testing.T) {
	t.Parallel()

	m := newApplicationDetailModel(documents.NewStore(t.TempDir()), testApplicationView())
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, runeKey('b')} {
		cmd, intent := m.Update(key)
		if cmd != nil {
			t.Fatalf("cmd = %v, want nil", cmd)
		}
		if _, ok := intent.(backToActiveApplicationsMsg); !ok {
			t.Fatalf("intent = %T, want backToActiveApplicationsMsg", intent)
		}
	}
}

func TestApplicationDetailModel_P_ReturnsEnterPostingDetailMsg(t *testing.T) {
	t.Parallel()

	m := newApplicationDetailModel(documents.NewStore(t.TempDir()), testApplicationView())
	cmd, intent := m.Update(runeKey('p'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterPostingDetailMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterPostingDetailMsg", intent)
	}
	if got.postingID != 5 {
		t.Fatalf("postingID = %d, want 5 (the application's posting)", got.postingID)
	}
}

func TestApplicationDetailModel_L_ReturnsOpenCoverLetterCmd(t *testing.T) {
	t.Parallel()

	m := newApplicationDetailModel(documents.NewStore(t.TempDir()), testApplicationView())
	cmd, intent := m.Update(runeKey('l'))
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that opens the cover letter in $EDITOR")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestApplicationDetailModel_R_ReturnsOpenResumeCmd(t *testing.T) {
	t.Parallel()

	m := newApplicationDetailModel(documents.NewStore(t.TempDir()), testApplicationView())
	cmd, intent := m.Update(runeKey('r'))
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that opens the resume in $EDITOR")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestApplicationDetailModel_ShiftL_OnMissingCoverLetter_NoOp(t *testing.T) {
	t.Parallel()

	m := newApplicationDetailModel(documents.NewStore(t.TempDir()), testApplicationView())
	cmd, intent := m.Update(runeKey('L'))
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (cover letter doesn't exist yet)", cmd, intent)
	}
}

func TestApplicationDetailModel_ShiftL_OnExistingCoverLetter_ReturnsEnterDocumentReviewFormMsg(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docs := documents.NewStore(dir)
	if _, err := docs.EnsureDir(1); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	want := "Dear hiring manager, I am excited to apply."
	if err := os.WriteFile(filepath.Join(dir, "1", "cover_letter.md"), []byte(want), 0o644); err != nil {
		t.Fatalf("write cover letter: %v", err)
	}

	m := newApplicationDetailModel(docs, testApplicationView())
	cmd, intent := m.Update(runeKey('L'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterDocumentReviewFormMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterDocumentReviewFormMsg", intent)
	}
	if got.err != nil {
		t.Fatalf("err = %v, want nil", got.err)
	}
	if got.applicationID != 1 {
		t.Fatalf("applicationID = %d, want 1", got.applicationID)
	}
	if got.documentType != store.DocumentTypeCoverLetter {
		t.Fatalf("documentType = %v, want %v", got.documentType, store.DocumentTypeCoverLetter)
	}
	if got.content != want {
		t.Fatalf("content = %q, want %q", got.content, want)
	}
}

func TestApplicationDetailModel_ShiftR_OnExistingResume_ReturnsEnterDocumentReviewFormMsg(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docs := documents.NewStore(dir)
	if _, err := docs.EnsureDir(1); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	want := "# Resume"
	if err := os.WriteFile(filepath.Join(dir, "1", "resume.md"), []byte(want), 0o644); err != nil {
		t.Fatalf("write resume: %v", err)
	}

	m := newApplicationDetailModel(docs, testApplicationView())
	cmd, intent := m.Update(runeKey('R'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterDocumentReviewFormMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterDocumentReviewFormMsg", intent)
	}
	if got.documentType != store.DocumentTypeResume {
		t.Fatalf("documentType = %v, want %v", got.documentType, store.DocumentTypeResume)
	}
	if got.content != want {
		t.Fatalf("content = %q, want %q", got.content, want)
	}
}

func TestApplicationDetailModel_View_ShowsOutcomeAndNotes(t *testing.T) {
	t.Parallel()

	application := testApplicationView()
	application.LatestReviews = map[store.DocumentType]store.DocumentReview{
		store.DocumentTypeCoverLetter: {Outcome: store.ReviewOutcomeFlagged, Notes: "too generic"},
		store.DocumentTypeResume:      {Outcome: store.ReviewOutcomePassed},
	}
	m := newApplicationDetailModel(documents.NewStore(t.TempDir()), application)

	got := m.View()
	if !containsAll(got, "Engineer", "Acme", "[FLAGGED]", "too generic", "[PASSED]") {
		t.Fatalf("View() = %q, want title, company, and both review outcomes with notes", got)
	}
}
