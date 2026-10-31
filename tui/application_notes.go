package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationNotesModel edits one application's notes and saves them.
type applicationNotesModel struct {
	applications *service.ApplicationService
	application  message.Application
	textarea     textarea.Model
	width        int
	focused      bool
	instance     message.ScreenInstance
	// seed is the notes the screen opened with, to tell whether esc would
	// lose edits.
	seed              string
	confirmingDiscard bool
	// saving is set from ctrl+s until the save fails; keys are ignored
	// meanwhile.
	saving bool
}

// newApplicationNotesModel opens on app's notes, sized to size. The width
// is fixed at construction.
func newApplicationNotesModel(applications *service.ApplicationService, app message.Application, size message.ContentSize) *applicationNotesModel {
	ta := textarea.New()
	ta.SetWidth(size.Width)
	ta.SetValue(app.Notes)
	ta.Focus()
	m := &applicationNotesModel{applications: applications, application: app, textarea: ta, width: size.Width, instance: message.NewScreenInstance(), seed: ta.Value()}
	m.setHeight(size.Rows)
	return m
}

func (m *applicationNotesModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *applicationNotesModel) Instance() message.ScreenInstance { return m.instance }

func (m *applicationNotesModel) Focused() bool { return m.focused }

func (m *applicationNotesModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.saved, model.From(m.instance)),
		model.Always(m.saveFailed, model.From(m.instance)),
		model.WhenFocused(m, m.keyPressed),
		model.WhenFocused(m, m.paste),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *applicationNotesModel) saved(message.Event[message.Changed[message.Application]]) tea.Cmd {
	return m.close()
}

// saveFailed lets editing go on; the banner shows why.
func (m *applicationNotesModel) saveFailed(message.Event[message.Error]) tea.Cmd {
	m.saving = false
	return nil
}

func (m *applicationNotesModel) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the editor handles is registered.
func (m *applicationNotesModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *applicationNotesModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.setHeight(e.Payload.Value.Rows)
	return nil
}

func (m *applicationNotesModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

// close takes a failed save's error from the banner with the editor.
func (m *applicationNotesModel) close() tea.Cmd {
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), ""))
}

// setHeight fits the screen into height terminal rows, giving the text
// area whatever the help line, measured as rendered, leaves
// (issue #138).
func (m *applicationNotesModel) setHeight(height int) {
	m.textarea.SetHeight(max(height-lipgloss.Height(applicationNotesHelp()), 0))
}

func applicationNotesHelp() string { return model.HelpStyle.Render("ctrl+s: save  esc: cancel") }

// discardBox is the box drawn over the screen while esc's discard
// question is open, at most width wide.
func (m *applicationNotesModel) discardBox(width int) string {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Discard unsaved changes?") + "\n")
	b.WriteString("Your edits to the notes aren't saved.\n\n")
	b.WriteString(model.FieldLabel.Render("y") + ": discard  " + model.FieldLabel.Render("n/esc") + ": keep editing  " + model.FieldLabel.Render("ctrl+s") + ": save")
	return confirmBox(b.String(), width)
}

func (m *applicationNotesModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	if m.saving {
		return nil
	}
	if m.confirmingDiscard {
		switch {
		case msg.String() == "y":
			return m.close()
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
		return m.close()
	case "ctrl+s":
		m.saving = true
		applications, src, id, notes := m.applications, m.src(), m.application.ID, m.textarea.Value()
		return func() tea.Msg {
			a, err := applications.SetNotes(context.Background(), id, notes)
			return applicationWritten(src, id, message.OpUpdated, a, err)
		}
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd
}

func (m *applicationNotesModel) View() tea.View {
	var b strings.Builder
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(applicationNotesHelp())
	body := b.String()
	if m.confirmingDiscard {
		body = overlay(body, m.discardBox(m.width), m.width)
	}
	return tea.NewView(body)
}

func (m *applicationNotesModel) paste(msg tea.PasteMsg) tea.Cmd {
	if m.saving || m.confirmingDiscard {
		return nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd
}
