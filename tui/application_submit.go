package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationSubmitModel drives the submit screen (#166), reached with S
// on application detail. Opening it opens the posting's apply link and
// exports the application's documents. The user fills in the form in the
// browser, then confirms here to mark the application submitted, or backs
// out with the status unchanged. Nothing is ever submitted to the job
// board: Swamp only records that the user did it (RFC 0002, "out of
// scope").
type applicationSubmitModel struct {
	applications    *service.ApplicationService
	documentService *service.DocumentService
	documents       *documents.Store
	openURL         func(src message.Source, about message.Subject, url string) tea.Cmd

	detail message.ApplicationDetail
	// dir is where the documents are exported to.
	dir string
	// url is the link opened for the user, "" when the posting has none.
	url string
	// opened is whether the link opened; openFailed is why it didn't. Both
	// are zero until the browser reports.
	opened     bool
	openFailed string
	// exported is the export's report, nil until it arrives; exportFailed
	// is why it failed instead.
	exported     *message.ExportReport
	exportFailed string
	// confirmed is set once y is pressed, so a second y while the status
	// save is in flight doesn't save it twice.
	confirmed bool

	focused  bool
	instance message.ScreenInstance
}

// applyURL is where a posting's application form is: ApplicationURL,
// falling back to JobURL when the board didn't give one.
func applyURL(p message.Posting) string {
	if p.ApplicationURL != "" {
		return p.ApplicationURL
	}
	return p.JobURL
}

func newApplicationSubmitModel(applications *service.ApplicationService, documentService *service.DocumentService, docs *documents.Store,
	openURL func(message.Source, message.Subject, string) tea.Cmd, d message.ApplicationDetail, dir string) *applicationSubmitModel {
	return &applicationSubmitModel{
		applications: applications, documentService: documentService, documents: docs, openURL: openURL,
		detail: d, dir: dir, url: applyURL(d.Posting), instance: message.NewScreenInstance(),
	}
}

func (m *applicationSubmitModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *applicationSubmitModel) Instance() message.ScreenInstance { return m.instance }

func (m *applicationSubmitModel) Focused() bool { return m.focused }

func (m *applicationSubmitModel) Routes() []*model.Route {
	return []*model.Route{
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.browserOpened, model.From(m.instance)),
		model.Always(m.exportRan, model.From(m.instance)),
		model.Always(m.failed, model.From(m.instance)),
		model.Always(m.submitted, model.From(m.instance)),
		model.Always(m.focusChanged),
	}
}

// Init starts the first two steps together: opening the apply link and
// exporting the documents, recorded like any other export (#188).
func (m *applicationSubmitModel) Init() tea.Cmd {
	export := exportApplicationDocuments(m.documentService, m.documents, m.src(), m.detail, m.dir)
	if m.url == "" {
		return export
	}
	return tea.Batch(m.openURL(m.src(), message.Subject{Type: message.TypePosting, ID: m.detail.Posting.ID}, m.url), export)
}

// Update has nothing to do: everything the screen handles is registered.
func (m *applicationSubmitModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *applicationSubmitModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

func (m *applicationSubmitModel) browserOpened(message.Event[message.Ran[message.OpenBrowser]]) tea.Cmd {
	m.opened = true
	return nil
}

func (m *applicationSubmitModel) exportRan(e message.Event[message.Ran[message.ExportReport]]) tea.Cmd {
	m.exported = &e.Payload.Value
	return nil
}

// failed shows a failed step on the screen, as well as in the banner, and
// stays: the user still decides whether they submitted. A failed save
// takes back its y, so the next one tries again.
func (m *applicationSubmitModel) failed(e message.Event[message.Error]) tea.Cmd {
	switch {
	case e.Meta.Subject.Type == message.TypePosting:
		m.openFailed = e.Payload.Text
	case e.Meta.Op == message.OpNone:
		m.exportFailed = e.Payload.Text
	default:
		m.confirmed = false
	}
	return nil
}

// submitted closes the screen once its own save goes through.
func (m *applicationSubmitModel) submitted(message.Event[message.Changed[message.Application]]) tea.Cmd {
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), "Marked "+m.detail.Posting.Title+" submitted"))
}

func (m *applicationSubmitModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case msg.String() == "esc", msg.String() == "n":
		return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), ""))
	case msg.String() == "y" && !m.confirmed:
		m.confirmed = true
		return updateApplicationStatus(m.applications, m.src(), m.detail.Application.ID, application.Submitted)
	}
	return nil
}

func (m *applicationSubmitModel) View() tea.View {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Company:") + " " + m.detail.CompanyName + "\n\n")

	b.WriteString(model.FieldLabel.Render("1. Apply link:") + " ")
	switch {
	case m.url == "":
		b.WriteString("this posting has no link to open\n")
	case m.openFailed != "":
		b.WriteString(model.ErrStyle.Render("couldn't open "+m.url+": "+m.openFailed) + "\n")
	case m.opened:
		b.WriteString("opened " + m.url + "\n")
	default:
		b.WriteString("opening " + m.url + "...\n")
	}

	b.WriteString(model.FieldLabel.Render("2. Documents:") + " ")
	switch {
	case m.exportFailed != "":
		b.WriteString(model.ErrStyle.Render("export failed: "+m.exportFailed) + "\n")
	case m.exported == nil:
		b.WriteString("exporting...\n")
	default:
		b.WriteString(exportStatusLine(*m.exported) + "\n")
		for _, path := range m.exported.Paths {
			b.WriteString("  " + path + "\n")
		}
	}
	reviews := reviewsOf(message.ReviewList{Reviews: m.detail.Reviews})
	for _, documentType := range document.Types() {
		review, ok := reviews[documentType]
		b.WriteString("  " + documentType.Label() + ": " + reviewBadge(review, ok) + "\n")
	}

	b.WriteString("\n" + model.FieldLabel.Render("3.") + " Fill in the form in your browser and submit it there.\n\n")
	if m.confirmed {
		b.WriteString("Marking submitted...\n")
	} else {
		b.WriteString("Did you submit it? " + model.FieldLabel.Render("y") + ": mark submitted  " + model.FieldLabel.Render("n/esc") + ": not yet, leave it as is\n")
	}
	return tea.NewView(b.String())
}
