package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationFormModel drives the screen for entering a posting's
// application form by hand (#184): whether each document is required,
// optional or absent, and the custom questions, as the text
// jobboard.ParseFormText reads. It's for boards whose forms Swamp can't
// fetch (Ashby, Lever); stage_prepare returns what's saved here the same
// way it returns a fetched Greenhouse form.
type applicationFormModel struct {
	postings    *service.PostingService
	application message.Application
	// previous is the stored form being edited, so questions the user
	// didn't change keep the Type and Options the text doesn't show.
	previous jobboard.ApplicationForm
	textarea textarea.Model
	// parseErr is why the last ctrl+s didn't save. The screen stays open
	// so the user can fix the line it names.
	parseErr error
	// seed is the text the screen opened with, to tell whether esc would
	// lose edits.
	seed string
	// loaded is set once the stored form is read; until then the text
	// area holds nothing to edit.
	loaded            bool
	confirmingDiscard bool
	// saving is set from a valid ctrl+s until the save fails; keys are
	// ignored meanwhile.
	saving   bool
	width    int
	focused  bool
	instance message.ScreenInstance
}

// newApplicationFormModel opens on app, sized to size. The stored form
// comes with Init. The width is fixed at construction.
func newApplicationFormModel(postings *service.PostingService, app message.Application, size message.ContentSize) *applicationFormModel {
	ta := textarea.New()
	ta.SetWidth(size.Width)
	ta.Focus()
	m := &applicationFormModel{postings: postings, application: app, textarea: ta, width: size.Width, instance: message.NewScreenInstance()}
	m.setHeight(size.Rows)
	return m
}

func (m *applicationFormModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *applicationFormModel) Instance() message.ScreenInstance { return m.instance }

func (m *applicationFormModel) Focused() bool { return m.focused }

func (m *applicationFormModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.formRead, model.From(m.instance)),
		model.Always(m.saved, model.From(m.instance)),
		model.Always(m.failed, model.From(m.instance)),
		model.WhenFocused(m, m.keyPressed),
		model.WhenFocused(m, m.paste),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *applicationFormModel) Init() tea.Cmd {
	return loadApplicationForm(m.postings, m.src(), m.application.PostingID)
}

// Update has nothing to do: everything the form handles is registered.
func (m *applicationFormModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *applicationFormModel) formRead(e message.Event[message.Read[message.ApplicationForm]]) tea.Cmd {
	m.previous = applicationFormOf(e.Payload.Value)
	m.textarea.SetValue(jobboard.FormText(m.previous))
	m.seed, m.loaded = m.textarea.Value(), true
	return nil
}

func (m *applicationFormModel) saved(message.Event[message.Ran[message.ApplicationForm]]) tea.Cmd {
	return m.close("Application form saved")
}

// failed lets editing go on after a failed save; the banner shows why. A
// form that couldn't be read leaves the error on the banner, since
// there's nothing to edit.
func (m *applicationFormModel) failed(e message.Event[message.Error]) tea.Cmd {
	if !m.loaded {
		return message.Pop(m.src())
	}
	m.saving = false
	return nil
}

func (m *applicationFormModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.setHeight(e.Payload.Value.Rows)
	return nil
}

func (m *applicationFormModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

// close takes a failed save's error from the banner with the form.
func (m *applicationFormModel) close(status string) tea.Cmd {
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), status))
}

// setHeight fits the screen into height rows, giving the text area what
// the intro, error and help lines leave (issue #138).
func (m *applicationFormModel) setHeight(height int) {
	m.textarea.SetHeight(max(height-lipgloss.Height(applicationFormIntro())-lipgloss.Height(applicationFormHelp())-1, 0))
}

func applicationFormIntro() string {
	return model.DimStyle.Render("What the apply page asks for. Paste its questions below.")
}

func applicationFormHelp() string { return model.HelpStyle.Render("ctrl+s: save  esc: cancel") }

// discardBox is the box drawn over the screen while esc's discard
// question is open, at most width wide.
func (m *applicationFormModel) discardBox(width int) string {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Discard unsaved changes?") + "\n")
	b.WriteString("Your edits to the application form aren't saved.\n\n")
	b.WriteString(model.FieldLabel.Render("y") + ": discard  " + model.FieldLabel.Render("n/esc") + ": keep editing  " + model.FieldLabel.Render("ctrl+s") + ": save")
	return confirmBox(b.String(), width)
}

func (m *applicationFormModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	if !m.loaded {
		if msg.String() == "esc" {
			return m.close("")
		}
		return nil
	}
	if m.saving {
		return nil
	}
	if m.confirmingDiscard {
		switch {
		case msg.String() == "y":
			return m.close("")
		case msg.String() == "esc", msg.String() == "n":
			m.confirmingDiscard = false
			return nil
		case msg.String() != "ctrl+s":
			return nil
		}
		m.confirmingDiscard = false
	}
	switch msg.String() {
	case "esc":
		if m.textarea.Value() != m.seed {
			m.confirmingDiscard = true
			return nil
		}
		return m.close("")
	case "ctrl+s":
		form, err := jobboard.ParseFormText(m.textarea.Value(), &m.previous)
		m.parseErr = err
		if err != nil {
			return nil
		}
		m.saving = true
		return saveApplicationForm(m.postings, m.src(), m.application.PostingID, form)
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd
}

func (m *applicationFormModel) paste(msg tea.PasteMsg) tea.Cmd {
	if !m.loaded || m.saving || m.confirmingDiscard {
		return nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd
}

func (m *applicationFormModel) crumbs(message.Screen) []string {
	return []string{"Application form"}
}

func (m *applicationFormModel) View() tea.View {
	var b strings.Builder
	b.WriteString(applicationFormIntro() + "\n")
	if m.parseErr != nil {
		b.WriteString(model.ErrStyle.Render(m.parseErr.Error()))
	}
	b.WriteString("\n")
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(applicationFormHelp())
	body := b.String()
	if m.confirmingDiscard {
		body = overlay(body, m.discardBox(m.width), m.width)
	}
	return tea.NewView(body)
}

// loadApplicationForm reads postingID's stored form, fetched or entered by
// hand. A posting with none reads as an empty form.
func loadApplicationForm(postings *service.PostingService, src message.Source, postingID int64) tea.Cmd {
	subject := message.Subject{Type: message.TypePosting, ID: postingID}
	return func() tea.Msg {
		read := func(form jobboard.ApplicationForm) tea.Msg {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.ApplicationForm]{Value: applicationFormSnapshot(form)})
		}
		fail := func(err error) tea.Msg {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		stored, err := postings.Form(context.Background(), postingID)
		if errors.Is(err, store.ErrNotFound) {
			return read(jobboard.ApplicationForm{})
		}
		if err != nil {
			return fail(err)
		}
		var form jobboard.ApplicationForm
		if err := json.Unmarshal([]byte(stored.JSON), &form); err != nil {
			return fail(fmt.Errorf("tui: decode stored application form: %w", err))
		}
		return read(form)
	}
}

// saveApplicationForm stores form as postingID's application form, where
// stage_prepare reads it (#168, #184). It reports a Ran rather than a
// Changed, since no other screen shows the form.
func saveApplicationForm(postings *service.PostingService, src message.Source, postingID int64, form jobboard.ApplicationForm) tea.Cmd {
	subject := message.Subject{Type: message.TypePosting, ID: postingID}
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return message.New(src, subject, message.OpUpdated, time.Now().UTC(), model.ErrorOf(err))
		}
		encoded, err := json.Marshal(form)
		if err != nil {
			return fail(fmt.Errorf("tui: encode application form: %w", err))
		}
		if err := postings.SaveForm(context.Background(), postingID, string(encoded)); err != nil {
			return fail(err)
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Ran[message.ApplicationForm]{Value: applicationFormSnapshot(form)})
	}
}

var requirementSnapshots = map[jobboard.Requirement]message.FormRequirement{
	jobboard.Optional: message.FormOptional,
	jobboard.Required: message.FormRequired,
}

// applicationFormSnapshot leaves out the documents form doesn't ask for.
func applicationFormSnapshot(form jobboard.ApplicationForm) message.ApplicationForm {
	var s message.ApplicationForm
	for documentType, requirement := range form.Documents {
		if snapshot, ok := requirementSnapshots[requirement]; ok {
			if s.Documents == nil {
				s.Documents = map[message.DocumentType]message.FormRequirement{}
			}
			s.Documents[documentTypeSnapshots[documentType]] = snapshot
		}
	}
	for _, q := range form.Questions {
		s.Questions = append(s.Questions, message.FormQuestion{Label: q.Label, Required: q.Required, Type: q.Type, Options: q.Options})
	}
	return s
}

func applicationFormOf(s message.ApplicationForm) jobboard.ApplicationForm {
	form := jobboard.ApplicationForm{Documents: map[document.Type]jobboard.Requirement{}}
	for documentType, snapshot := range s.Documents {
		for requirement, r := range requirementSnapshots {
			if r == snapshot {
				form.Documents[documentTypeOf(documentType)] = requirement
			}
		}
	}
	for _, q := range s.Questions {
		form.Questions = append(form.Questions, jobboard.Question{Label: q.Label, Required: q.Required, Type: q.Type, Options: q.Options})
	}
	return form
}
