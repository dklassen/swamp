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

func TestApplicationDetailModel_OpenDocument_ReturnsEditorCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  rune
	}{
		{name: "l opens cover letter", key: 'l'},
		{name: "r opens resume", key: 'r'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newApplicationDetailModel(documents.NewStore(t.TempDir()), testApplicationView())
			cmd, intent := m.Update(runeKey(tt.key))
			if cmd == nil {
				t.Fatal("cmd = nil, want a command that opens the document in $EDITOR")
			}
			if intent != nil {
				t.Fatalf("intent = %v, want nil", intent)
			}
		})
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

func TestApplicationDetailModel_EnterReview_OnExistingDocument_ReturnsEnterDocumentReviewFormMsg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		key          rune
		filename     string
		content      string
		documentType store.DocumentType
	}{
		{
			name:         "shift+L reviews cover letter",
			key:          'L',
			filename:     "cover_letter.md",
			content:      "Dear hiring manager, I am excited to apply.",
			documentType: store.DocumentTypeCoverLetter,
		},
		{
			name:         "shift+R reviews resume",
			key:          'R',
			filename:     "resume.md",
			content:      "# Resume",
			documentType: store.DocumentTypeResume,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			docs := documents.NewStore(dir)
			if _, err := docs.EnsureDir(1); err != nil {
				t.Fatalf("EnsureDir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "1", tt.filename), []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write %s: %v", tt.filename, err)
			}

			m := newApplicationDetailModel(docs, testApplicationView())
			cmd, intent := m.Update(runeKey(tt.key))
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
			if got.documentType != tt.documentType {
				t.Fatalf("documentType = %v, want %v", got.documentType, tt.documentType)
			}
			if got.content != tt.content {
				t.Fatalf("content = %q, want %q", got.content, tt.content)
			}
		})
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
