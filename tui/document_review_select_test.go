package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/tui/message"
)

var testReviewApplication = message.Application{ID: 1, PostingID: 5, Status: message.ApplicationStarted}

// reviewPickerWithCoverLetter is a picker over a store where the
// application's cover letter holds content and its resume isn't drafted.
func reviewPickerWithCoverLetter(t *testing.T, content string) *documentReviewSelectModel {
	t.Helper()
	docs := documents.NewStore(t.TempDir())
	if _, err := docs.Write(testReviewApplication.ID, document.CoverLetter, content); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return newDocumentReviewSelectModel(docs, testReviewApplication)
}

func TestDocumentReviewSelectModel_New_SeedsOptionsFromDocumentStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docs := documents.NewStore(dir)
	if _, err := docs.EnsureDir(1); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1", "cover_letter.md"), []byte("Dear hiring manager"), 0o644); err != nil {
		t.Fatalf("write cover letter: %v", err)
	}

	m := newDocumentReviewSelectModel(docs, testReviewApplication)

	if len(m.options) != 2 {
		t.Fatalf("len(options) = %d, want 2", len(m.options))
	}
	if m.options[0].documentType != document.CoverLetter || !m.options[0].exists {
		t.Fatalf("options[0] = %+v, want cover_letter, exists=true", m.options[0])
	}
	if m.options[1].documentType != document.Resume || m.options[1].exists {
		t.Fatalf("options[1] = %+v, want resume, exists=false", m.options[1])
	}
}

func TestDocumentReviewSelectModel_CursorMovement_ClampsToOptions(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewSelectModel(documents.NewStore(t.TempDir()), testReviewApplication)

	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyDown})
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor after two downs = %d, want 1 (clamped)", m.cursor)
	}
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyUp})
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor after two ups = %d, want 0 (clamped)", m.cursor)
	}
}

func TestDocumentReviewSelectModel_EscOrB_PopsAndClearsTheBanner(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('b')} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			m := newDocumentReviewSelectModel(documents.NewStore(t.TempDir()), testReviewApplication)

			got := runCmd(t, m.keyPressed(key))
			if !popsIn(got, m.Instance()) {
				t.Errorf("%s sent %#v, want a pop of the picker", key, got)
			}
			if status, ok := statusIn(got); !ok || status != "" {
				t.Errorf("%s sent %#v, want an empty Notify", key, got)
			}
		})
	}
}

func TestDocumentReviewSelectModel_Enter_OnMissingDocument_DoesNothing(t *testing.T) {
	t.Parallel()

	m := reviewPickerWithCoverLetter(t, "a draft")
	m.keyPressed(runeKey('j')) // the resume, not drafted
	if got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})); len(got) != 0 {
		t.Fatalf("enter on a missing document sent %#v, want nothing", got)
	}
}

// The picker gives way to the form, opened on the document as it was read
// when it was picked.
func TestDocumentReviewSelectModel_Enter_OnExistingDocument_ReplacesItselfWithTheForm(t *testing.T) {
	t.Parallel()

	want := "Dear hiring manager, I am excited to apply."
	m := reviewPickerWithCoverLetter(t, want)

	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}))

	if len(got) != 1 {
		t.Fatalf("enter sent %#v, want one Nav", got)
	}
	nav, ok := got[0].(message.Event[message.Nav[message.DocumentReview]])
	if !ok {
		t.Fatalf("enter sent %T, want a Nav to the review form", got[0])
	}
	assertEventFrom(t, nav.Meta, message.Source{Instance: m.Instance()})
	if want := (message.Subject{Type: message.TypeApplication, ID: testReviewApplication.ID}); nav.Meta.Subject != want {
		t.Errorf("Subject = %+v, want %+v", nav.Meta.Subject, want)
	}
	wantNav := message.Nav[message.DocumentReview]{Op: message.NavReplace, Screen: message.ScreenDocumentReviewForm,
		Value: message.DocumentReview{ApplicationID: testReviewApplication.ID, DocumentType: message.DocumentCoverLetter, Content: want}}
	if diff := cmp.Diff(wantNav, nav.Payload); diff != "" {
		t.Errorf("Nav mismatch (-want +got):\n%s", diff)
	}
}

func TestDocumentReviewSelectModel_TakesKeysOnlyWhileFocused(t *testing.T) {
	t.Parallel()

	m := reviewPickerWithCoverLetter(t, "a draft")
	r := routed(t, m)
	r.Dispatch(focusOn(newDocumentReviewSelectModel(documents.NewStore(t.TempDir()), testReviewApplication).Instance()))

	r.Dispatch(runeKey('j'))

	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0: it moved while another screen was in focus", m.cursor)
	}
}

// The picker offers every document type in documents' table (RFC 0004).
func TestDocumentReviewSelectModel_OffersEveryDocumentType(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewSelectModel(documents.NewStore(t.TempDir()), testReviewApplication)
	var got []document.Type
	for _, option := range m.options {
		got = append(got, option.documentType)
	}
	if diff := cmp.Diff(document.Types(), got); diff != "" {
		t.Errorf("options mismatch (-want +got):\n%s", diff)
	}
}

func TestDocumentReviewSelectModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, newDocumentReviewSelectModel(documents.NewStore(t.TempDir()), testReviewApplication))
}
