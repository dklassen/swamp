package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/model"
)

// applicationNotesModel drives the application-notes-edit screen for a
// single application. It holds the service it needs to save notes, the
// application it's editing (fixed for this screen's lifetime), and its
// own private textarea -- no other screen reads or writes this state.
type applicationNotesModel struct {
	applications  *service.ApplicationService
	applicationID int64
	textarea      textarea.Model
	instance      screenInstance
	// seed is the notes the screen opened with, to tell whether esc would
	// lose edits.
	seed              string
	confirmingDiscard bool
	// saving is set from ctrl+s until App reports the save failed (on
	// success App leaves the screen); keys are ignored meanwhile.
	saving bool
}

// newApplicationNotesModel returns a notes-edit screen for applicationID,
// seeded with notes and sized to width and height, the rows App leaves
// under its status/error banner (App.screenRows). App refits the height
// with setHeight whenever that changes; the width is fixed at
// construction.
func newApplicationNotesModel(applications *service.ApplicationService, applicationID int64, notes string, width, height int, instance screenInstance) applicationNotesModel {
	ta := textarea.New()
	ta.SetWidth(width)
	ta.SetValue(notes)
	ta.Focus()
	m := applicationNotesModel{applications: applications, applicationID: applicationID, textarea: ta, instance: instance, seed: ta.Value()}
	m.setHeight(height)
	return m
}

// setHeight fits the screen into height terminal rows, giving the text
// area whatever the help line, measured as rendered, leaves
// (issue #138).
func (m *applicationNotesModel) setHeight(height int) {
	m.textarea.SetHeight(max(height-lipgloss.Height(applicationNotesHelp()), 0))
}

func applicationNotesHelp() string { return model.HelpStyle.Render("ctrl+s: save  esc: cancel") }

// discardBox is the box App draws over the screen while esc's discard
// question is open, at most width wide.
func (m *applicationNotesModel) discardBox(width int) string {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Discard unsaved changes?") + "\n")
	b.WriteString("Your edits to the notes aren't saved.\n\n")
	b.WriteString(model.FieldLabel.Render("y") + ": discard  " + model.FieldLabel.Render("n/esc") + ": keep editing  " + model.FieldLabel.Render("ctrl+s") + ": save")
	return confirmBox(b.String(), width)
}

// cancelApplicationNotesMsg signals that App should switch back to the
// posting-detail screen without saving.
type cancelApplicationNotesMsg struct{}

func (m *applicationNotesModel) Update(msg tea.KeyPressMsg) (tea.Cmd, tea.Msg) {
	if m.saving {
		return nil, nil
	}
	if m.confirmingDiscard {
		switch {
		case msg.String() == "y":
			return nil, cancelApplicationNotesMsg{}
		case msg.String() == "esc", msg.String() == "n":
			m.confirmingDiscard = false
			return nil, nil
		case msg.String() != "ctrl+s":
			return nil, nil
		}
		m.confirmingDiscard = false
	}
	switch msg.String() {
	case "esc":
		if m.textarea.Value() != m.seed {
			m.confirmingDiscard = true
			return nil, nil
		}
		return nil, cancelApplicationNotesMsg{}
	case "ctrl+s":
		m.saving = true
		return updateApplicationNotes(m.applications, m.applicationID, m.textarea.Value(), m.instance), nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd, nil
}

func (m *applicationNotesModel) View() string {
	var b strings.Builder
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(applicationNotesHelp())
	return b.String()
}

func (m *applicationNotesModel) paste(msg tea.PasteMsg) tea.Cmd {
	if m.saving || m.confirmingDiscard {
		return nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd
}
