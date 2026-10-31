package tui

import (
	"context"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationDetailModel is one application, where its own actions live:
// its posting, editing and reviewing each document, its form, submitting
// and deleting it. It reads the application, which it keeps.
type applicationDetailModel struct {
	applications    *service.ApplicationService
	postings        *service.PostingService
	companies       *service.CompanyService
	documentService *service.DocumentService
	documents       *documents.Store

	detail message.ApplicationDetail
	// deleted is set once its application is, so it leaves rather than
	// reading what's gone.
	deleted bool
	reading reading
	// confirm is the delete being confirmed, or nil.
	confirm *applicationDeleteModel

	width    int
	focused  bool
	instance message.ScreenInstance
}

func newApplicationDetailModel(applications *service.ApplicationService, postings *service.PostingService, companies *service.CompanyService, documentService *service.DocumentService, docs *documents.Store, d message.ApplicationDetail, width int) *applicationDetailModel {
	return &applicationDetailModel{
		applications:    applications,
		postings:        postings,
		companies:       companies,
		documentService: documentService,
		documents:       docs,
		detail:          d,
		width:           width,
		instance:        message.NewScreenInstance(),
	}
}

func (m *applicationDetailModel) Instance() message.ScreenInstance { return m.instance }

func (m *applicationDetailModel) Focused() bool { return m.focused }

func (m *applicationDetailModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.detailRead, model.From(m.instance)),
		model.Always(m.readFailed, model.From(m.instance)),
		model.Always(m.deleteFailed),
		model.Always(m.applicationChanged, model.About(message.TypeApplication, m.detail.Application.ID)),
		model.Always(m.reviewChanged, model.About(message.TypeApplication, m.detail.Application.ID)),
		model.Always(m.documentEdited, model.From(m.instance)),
		model.Always(m.documentWritten, model.About(message.TypeApplication, m.detail.Application.ID)),
		model.Always(m.documentExported, model.About(message.TypeApplication, m.detail.Application.ID)),
		model.Always(m.postingChanged),
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *applicationDetailModel) Init() tea.Cmd { return m.load() }

// Update has nothing to do: everything the detail handles is registered.
func (m *applicationDetailModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *applicationDetailModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *applicationDetailModel) load() tea.Cmd {
	if m.deleted {
		return nil
	}
	return loadApplicationDetail(m.applications, m.postings, m.companies, m.documentService, m.documents, m.src(), m.detail.Application.ID)
}

func (m *applicationDetailModel) detailRead(e message.Event[message.Read[message.ApplicationDetail]]) tea.Cmd {
	m.detail = e.Payload.Value
	return m.reading.done(m.load)
}

// readFailed leaves an application that's gone, deleted elsewhere.
func (m *applicationDetailModel) readFailed(e message.Event[message.Error]) tea.Cmd {
	next := m.reading.done(m.load)
	if e.Payload.Code != message.ErrNotFound || m.deleted {
		return next
	}
	return m.goneElsewhere()
}

func (m *applicationDetailModel) goneElsewhere() tea.Cmd {
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), "The application you were viewing was deleted elsewhere"))
}

// applicationChanged takes a write to its application, whoever made it. A
// delete closes it, with its confirmation if that's open, or once it's
// back on top if another screen is over it.
func (m *applicationDetailModel) applicationChanged(e message.Event[message.Changed[message.Application]]) tea.Cmd {
	switch e.Meta.Op {
	case message.OpUpdated:
		m.detail.Application = e.Payload.After
	case message.OpDeleted:
		m.deleted = true
		if m.confirm != nil {
			cmd := m.confirm.deleted(e)
			m.confirm = nil
			return tea.Batch(message.Pop(m.src()), cmd)
		}
		if m.focused {
			return m.goneElsewhere()
		}
	}
	return nil
}

// deleteFailed closes the confirmation whose delete failed, since its y is
// spent, and leaves too if it found the application already gone.
func (m *applicationDetailModel) deleteFailed(e message.Event[message.Error]) tea.Cmd {
	if m.confirm == nil || e.Meta.Source.Instance != m.confirm.instance {
		return nil
	}
	cmd := m.confirm.failed(e)
	m.confirm = nil
	if e.Payload.Code != message.ErrNotFound {
		return cmd
	}
	m.deleted = true
	return tea.Batch(message.Pop(m.src()), cmd)
}

func (m *applicationDetailModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.width = e.Payload.Value.Width
	return nil
}

// reviewChanged takes a review of one of its documents, whoever saved it.
func (m *applicationDetailModel) reviewChanged(e message.Event[message.Changed[message.Review]]) tea.Cmd {
	m.detail.Reviews = withReview(m.detail.Reviews, e.Payload.After)
	return nil
}

// documentWritten and documentExported read again, since what's drafted
// and exported decides what's next.
func (m *applicationDetailModel) documentWritten(message.Event[message.Changed[message.DocumentWrite]]) tea.Cmd {
	return m.reading.start(m.load)
}

func (m *applicationDetailModel) documentExported(message.Event[message.Changed[message.DocumentExport]]) tea.Cmd {
	return m.reading.start(m.load)
}

// postingChanged reads again for its posting, which it learns only once
// it has read, so isn't filtered by About.
func (m *applicationDetailModel) postingChanged(e message.Event[message.Changed[message.Posting]]) tea.Cmd {
	if e.Meta.Subject.ID != m.detail.Posting.ID {
		return nil
	}
	return m.reading.start(m.load)
}

// documentEdited reads again, for the review an edit may have made stale.
func (m *applicationDetailModel) documentEdited(message.Event[message.Ran[message.EditDocument]]) tea.Cmd {
	return m.load()
}

// focusChanged reads again on coming back on top: documents change on
// disk with no event it hears.
func (m *applicationDetailModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	gained := !m.focused && e.Payload.Value.Focused == m.instance
	m.focused = e.Payload.Value.Focused == m.instance
	switch {
	case gained && m.deleted:
		return message.Pop(m.src())
	case gained:
		return m.load()
	}
	return nil
}

func (m *applicationDetailModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	if m.confirm != nil {
		cmd, closed := m.confirm.keyPressed(msg)
		if closed {
			m.confirm = nil
		}
		return cmd
	}
	switch msg.String() {
	case "esc", "b":
		return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), ""))
	case "p":
		return message.Push(m.src(), message.ScreenPostingDetail, message.Subject{Type: message.TypePosting, ID: m.detail.Posting.ID},
			message.PostingDetail{Posting: m.detail.Posting, Application: m.detail.Application})
	case "u":
		return m.load()
	case "S":
		return message.Push(m.src(), message.ScreenApplicationSubmit, message.Subject{Type: message.TypeApplication, ID: m.detail.Application.ID}, m.detail)
	case "f":
		return message.Push(m.src(), message.ScreenApplicationForm, message.Subject{Type: message.TypeApplication, ID: m.detail.Application.ID}, m.detail.Application)
	case "D":
		m.confirm = newApplicationDeleteModel(m.applications, m.detail)
		return nil
	}
	// Each document type's key edits it, and the uppercase key reviews it.
	for _, documentType := range document.Types() {
		switch msg.String() {
		case string(documentType.Key()):
			return editDocument(m.documentService, m.documents, m.src(), m.detail.Application.ID, documentType)
		case string(unicode.ToUpper(documentType.Key())):
			return m.openReview(documentType)
		}
	}
	return nil
}

// openReview opens the review form on documentType as it is now, or does
// nothing if it isn't drafted yet, since there's nothing to review.
func (m *applicationDetailModel) openReview(documentType document.Type) tea.Cmd {
	id := m.detail.Application.ID
	doc, err := m.documents.Status(id).Doc(documentType)
	if err != nil {
		return message.Fail(m.src(), err)
	}
	if !doc.Exists {
		return nil
	}
	return openDocumentReview(m.src(), message.Push[message.DocumentReview], id, documentType, doc.Path)
}

func (m *applicationDetailModel) crumbs(message.Screen) []string {
	return []string{m.detail.CompanyName, m.detail.Posting.Title}
}

func (m *applicationDetailModel) View() tea.View {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Company:") + " " + m.detail.CompanyName + "\n")
	b.WriteString(model.FieldLabel.Render("Status:") + " " + applicationStatusLabel(applicationOf(m.detail.Application).Status) + "\n")
	if m.detail.Application.Notes != "" {
		b.WriteString(model.FieldLabel.Render("Notes:") + " " + m.detail.Application.Notes + "\n")
	}

	status := m.documents.Status(m.detail.Application.ID)
	reviews := reviewsOf(message.ReviewList{Reviews: m.detail.Reviews})
	b.WriteString("\n" + model.FieldLabel.Render("Documents") + "\n")
	for _, documentType := range document.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			continue
		}
		presence := "not found"
		if doc.Exists {
			presence = "found"
		}
		review, hasReview := reviews[documentType]
		b.WriteString(model.FieldLabel.Render(documentTitle(documentType)+":") + " " + presence + " (" + doc.Path + ") " + reviewBadge(review, hasReview) + "\n")
		// The reviewer's record of what to fix before the next cycle.
		if hasReview && review.Notes != "" {
			b.WriteString("  " + model.DimStyle.Render("Notes: "+review.Notes) + "\n")
		}
	}

	help := []string{"p: view posting"}
	for _, documentType := range document.Types() {
		help = append(help, string(documentType.Key())+": edit "+documentType.Label())
	}
	for _, documentType := range document.Types() {
		help = append(help, string(unicode.ToUpper(documentType.Key()))+": review "+documentType.Label())
	}
	help = append(help, "f: application form", "S: submit", "D: delete", "u: refresh", "esc/b: back")
	b.WriteString(model.HelpStyle.Render(strings.Join(help, "  ")))
	if m.confirm != nil {
		return tea.NewView(overlay(b.String(), m.confirm.View(m.width), m.width))
	}
	return tea.NewView(b.String())
}

// applicationDetailOf is the snapshot detail opens on.
func applicationDetailOf(a posting.ActiveApplication) message.ApplicationDetail {
	d := message.ApplicationDetail{
		Application: applicationSnapshot(a.Application),
		Posting:     postingSnapshot(a.Posting),
		CompanyName: a.CompanyName,
	}
	for _, documentType := range document.Types() {
		if review, ok := a.LatestReviews[documentType]; ok {
			review.DocumentType = documentType
			d.Reviews = append(d.Reviews, reviewSnapshot(review))
		}
	}
	return d
}

// loadApplicationDetail reads an application with its posting, its
// company's name, and the reviews that still describe its documents.
func loadApplicationDetail(applications *service.ApplicationService, postings *service.PostingService, companies *service.CompanyService, documentService *service.DocumentService, docs *documents.Store, src message.Source, applicationID int64) tea.Cmd {
	subject := message.Subject{Type: message.TypeApplication, ID: applicationID}
	return func() tea.Msg {
		ctx := context.Background()
		fail := func(err error) tea.Msg {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		app, err := applications.Get(ctx, applicationID)
		if err != nil {
			return fail(err)
		}
		p, err := postings.Get(ctx, app.PostingID)
		if err != nil {
			return fail(err)
		}
		// The home screen lists an application whatever its company's state.
		c, err := companies.GetIncludingDeleted(ctx, p.CompanyID)
		if err != nil {
			return fail(err)
		}
		reviews, err := currentReviews(documentService, docs, applicationID)
		if err != nil {
			return fail(err)
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.ApplicationDetail]{
			Value: applicationDetailOf(posting.ActiveApplication{Application: *app, Posting: *p, CompanyName: c.Name, LatestReviews: reviews}),
		})
	}
}

// documentTitle is documentType's label in title case, for row headings
// ("Cover Letter").
func documentTitle(documentType document.Type) string {
	words := strings.Fields(documentType.Label())
	for i, word := range words {
		r := []rune(word)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
