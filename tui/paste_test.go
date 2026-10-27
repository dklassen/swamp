package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
)

func TestApp_PasteIntoApplicationForm_AddsTheText(t *testing.T) {
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, posting.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 80, Height: 30})
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, loadCmd := sendKey(app, runeKey('f'))
	app = applyCmd(t, app, loadCmd)

	app, _ = sendKey(app, tea.PasteMsg{Content: "Why us? *"})

	if view := viewText(app); !strings.Contains(view, "Why us? *") {
		t.Fatalf("pasted question isn't in the form:\n%s", view)
	}
}

func TestApplicationFormModel_PasteAtDiscardPrompt_IsIgnored(t *testing.T) {
	t.Parallel()

	m := newApplicationFormModel(nil, 7, nil, 80, 20)
	m.textarea.InsertString("Why us? *\n")
	typed := m.textarea.Value()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	m.paste(tea.PasteMsg{Content: "y"})

	if got := m.textarea.Value(); got != typed {
		t.Errorf("text = %q, want %q unchanged", got, typed)
	}
	if !m.confirmingDiscard {
		t.Error("a paste answered the discard prompt")
	}
}

func TestApp_Paste_GoesIntoTheScreensTextField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		open func(app *App)
	}{
		{name: "company form", open: func(app *App) {
			app.Update(message.OpenCompanyFormMsg{})
			// Past the source picker, which takes no text.
			app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		}},
		{name: "company edit", open: func(app *App) {
			app.currentScreen, app.companyEdit = screenCompanyEdit, newCompanyEditModel(nil, 1, "")
		}},
		{name: "export", open: func(app *App) {
			app.currentScreen, app.applicationExport = screenApplicationExport, newApplicationExportModel(nil, app.documents, posting.ActiveApplication{}, "", 80)
		}},
		{name: "notes", open: func(app *App) {
			app.currentScreen, app.applicationNotes = screenApplicationNotesEdit, newApplicationNotesModel(nil, 1, "", 80, 20)
		}},
		{name: "application form", open: func(app *App) {
			app.currentScreen, app.applicationForm = screenApplicationForm, newApplicationFormModel(nil, 1, nil, 80, 20)
		}},
		{name: "document review", open: func(app *App) {
			app.currentScreen, app.documentReviewForm = screenDocumentReviewForm, newDocumentReviewFormModel(nil, app.documents, 1, document.CoverLetter, "", 80, 20)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := New(nil, nil, nil, nil, nil, nil, nil, documents.NewStore(t.TempDir()))
			app, _ = sendKey(app, tea.WindowSizeMsg{Width: 100, Height: 30})
			tt.open(app)

			app, _ = sendKey(app, tea.PasteMsg{Content: "pasted"})

			if view := viewText(app); !strings.Contains(view, "pasted") {
				t.Errorf("pasted text isn't on the screen:\n%s", view)
			}
		})
	}
}

func TestApp_CompanyList_PasteIntoSearch_FiltersByIt(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	app, _ = sendKey(app, runeKey('/'))

	app, _ = sendKey(app, tea.PasteMsg{Content: "glo"})

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = applyCmd(t, app, cmd)
	if app.selectedCompany.Name != "Globex" {
		t.Fatalf("selectedCompany = %q, want %q", app.selectedCompany.Name, "Globex")
	}
}

func TestApp_CompanyList_PasteWithSearchClosed_IsNotACommand(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, cmd := sendKey(app, tea.PasteMsg{Content: "q"})

	if cmd != nil {
		t.Errorf("paste returned a command (%T), want none", cmd())
	}
	if app.currentScreen != screenCompanyList {
		t.Errorf("screen = %v, want the company list", app.currentScreen)
	}
}

func TestApp_ActiveApplications_PasteIntoSearch_FiltersByIt(t *testing.T) {
	t.Parallel()

	app := newFullTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 100, Height: 24})
	app = sendKeyAndApply(t, app, runeKey('/'))

	app, _ = sendKey(app, tea.PasteMsg{Content: "engineer 07"})

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenApplicationDetail || app.applicationDetail.application.Posting.Title != "Engineer 07" {
		t.Fatalf("after paste, enter, enter: screen %v showing %q, want application detail for Engineer 07", app.currentScreen, app.applicationDetail.application.Posting.Title)
	}
}

func TestAllPostings_PasteIntoSearch_SearchesForIt(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	mustUpsertPosting(t, s, acme.ID, "2", "Designer")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	app = sendKeyAndApply(t, app, runeKey('/'))

	app, cmd := sendKey(app, tea.PasteMsg{Content: "plat"})
	app = applyCmd(t, app, cmd)

	if diff := cmp.Diff([]string{"Platform Engineer"}, pageTitles(app)); diff != "" {
		t.Errorf("after pasting (-want +got):\n%s", diff)
	}
}
