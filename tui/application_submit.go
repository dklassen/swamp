package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationSubmitModel drives the submit screen (#166), reached with S
// on application detail. Entering it opens the posting's apply link and
// exports the application's documents (both started by App, see
// startApplicationSubmit). The user fills in the form in the browser,
// then confirms here to mark the application submitted, or backs out
// with the status unchanged. Nothing is ever submitted to the job board:
// Swamp only records that the user did it (RFC 0002, "out of scope").
type applicationSubmitModel struct {
	application posting.ActiveApplication
	// url is the link opened for the user, "" when the posting has none.
	url string
	// opened is the browser result, nil until it arrives.
	opened *browserOpenedMsg
	// exported is the export result, nil until it arrives.
	exported *applicationExportedMsg
	// confirmed is set once y is pressed, so a second y while the status
	// save is in flight doesn't save it twice.
	confirmed bool
	instance  message.ScreenInstance
}

// applyURL is where a posting's application form is: ApplicationURL,
// falling back to JobURL when the board didn't give one.
func applyURL(posting posting.Posting) string {
	if posting.ApplicationURL != "" {
		return posting.ApplicationURL
	}
	return posting.JobURL
}

func newApplicationSubmitModel(application posting.ActiveApplication, instance message.ScreenInstance) applicationSubmitModel {
	return applicationSubmitModel{application: application, url: applyURL(application.Posting), instance: instance}
}

// enterApplicationSubmitMsg signals that App should open the submit
// screen for this application.
type enterApplicationSubmitMsg struct {
	application posting.ActiveApplication
}

// cancelApplicationSubmitMsg signals that App should go back to
// application detail without changing anything.
type cancelApplicationSubmitMsg struct{}

func (m *applicationSubmitModel) Update(msg tea.KeyPressMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.String() == "esc", msg.String() == "n":
		return nil, cancelApplicationSubmitMsg{}
	case msg.String() == "y" && !m.confirmed:
		m.confirmed = true
		return nil, confirmApplicationSubmitMsg{applicationID: m.application.ID}
	}
	return nil, nil
}

// confirmApplicationSubmitMsg signals that App should mark the
// application submitted.
type confirmApplicationSubmitMsg struct {
	applicationID int64
}

func (m *applicationSubmitModel) View() string {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Company:") + " " + m.application.CompanyName + "\n\n")

	b.WriteString(model.FieldLabel.Render("1. Apply link:") + " ")
	switch {
	case m.url == "":
		b.WriteString("this posting has no link to open\n")
	case m.opened == nil:
		b.WriteString("opening " + m.url + "...\n")
	case m.opened.err != nil:
		b.WriteString(model.ErrStyle.Render("couldn't open "+m.url+": "+m.opened.err.Error()) + "\n")
	default:
		b.WriteString("opened " + m.url + "\n")
	}

	b.WriteString(model.FieldLabel.Render("2. Documents:") + " ")
	switch {
	case m.exported == nil:
		b.WriteString("exporting...\n")
	case m.exported.err != nil:
		b.WriteString(model.ErrStyle.Render("export failed: "+m.exported.err.Error()) + "\n")
	default:
		b.WriteString(exportStatusLine(*m.exported) + "\n")
		for _, path := range m.exported.paths {
			b.WriteString("  " + path + "\n")
		}
	}
	for _, documentType := range document.Types() {
		review, ok := m.application.LatestReviews[documentType]
		b.WriteString("  " + documentType.Label() + ": " + reviewBadge(review, ok) + "\n")
	}

	b.WriteString("\n" + model.FieldLabel.Render("3.") + " Fill in the form in your browser and submit it there.\n\n")
	if m.confirmed {
		b.WriteString("Marking submitted...\n")
	} else {
		b.WriteString("Did you submit it? " + model.FieldLabel.Render("y") + ": mark submitted  " + model.FieldLabel.Render("n/esc") + ": not yet, leave it as is\n")
	}
	return b.String()
}

// startApplicationSubmit switches to the submit screen for application
// and starts its first two steps together: opening the apply link and
// exporting the documents to the last-used export directory, recorded
// like any other export (#188).
func (a *App) startApplicationSubmit(application posting.ActiveApplication) tea.Cmd {
	a.applicationSubmit = newApplicationSubmitModel(application, a.newScreenInstance())
	a.currentScreen = screenApplicationSubmit
	export := exportApplicationDocuments(a.documentService, a.documents, application, a.exportDir)
	if a.applicationSubmit.url == "" {
		return export
	}
	return tea.Batch(a.openURL(a.applicationSubmit.url), export)
}
