package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dklassen/swamp/store"
)

// applicationNotesModel drives the application-notes-edit screen for a
// single posting's application. It holds the store it needs to save
// notes, the posting it's editing (fixed for this screen's lifetime),
// and its own private textarea -- no other screen reads or writes this
// state.
type applicationNotesModel struct {
	store     *store.Store
	postingID int64
	textarea  textarea.Model
	instance  screenInstance
	// seed is the notes the screen opened with, to tell whether esc would
	// lose edits.
	seed              string
	confirmingDiscard bool
	// saving is set from ctrl+s until App reports the save failed (on
	// success App leaves the screen); keys are ignored meanwhile.
	saving bool
}

// newApplicationNotesModel returns a notes-edit screen for postingID,
// seeded with notes and sized to width and height, the rows App leaves
// under its status/error banner (App.screenRows). App refits the height
// with setHeight whenever that changes; the width is fixed at
// construction.
func newApplicationNotesModel(s *store.Store, postingID int64, notes string, width, height int, instance screenInstance) applicationNotesModel {
	ta := textarea.New()
	ta.SetWidth(width)
	ta.SetValue(notes)
	ta.Focus()
	m := applicationNotesModel{store: s, postingID: postingID, textarea: ta, instance: instance, seed: ta.Value()}
	m.setHeight(height)
	return m
}

// setHeight fits the screen into height terminal rows, giving the text
// area whatever the help line, measured as rendered, leaves
// (issue #138).
func (m *applicationNotesModel) setHeight(height int) {
	m.textarea.SetHeight(max(height-lipgloss.Height(applicationNotesHelp()), 0))
}

func applicationNotesHelp() string { return helpStyle.Render("ctrl+s: save  esc: cancel") }

func applicationNotesDiscardPrompt() string {
	return helpStyle.Render("Discard unsaved changes? y: discard  n/esc: keep editing  ctrl+s: save")
}

// cancelApplicationNotesMsg signals that App should switch back to the
// posting-detail screen without saving.
type cancelApplicationNotesMsg struct{}

func (m *applicationNotesModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	if m.saving {
		return nil, nil
	}
	if m.confirmingDiscard {
		switch {
		case msg.String() == "y":
			return nil, cancelApplicationNotesMsg{}
		case msg.Type == tea.KeyEsc, msg.String() == "n":
			m.confirmingDiscard = false
			return nil, nil
		case msg.Type != tea.KeyCtrlS:
			return nil, nil
		}
		m.confirmingDiscard = false
	}
	switch msg.Type {
	case tea.KeyEsc:
		if m.textarea.Value() != m.seed {
			m.confirmingDiscard = true
			return nil, nil
		}
		return nil, cancelApplicationNotesMsg{}
	case tea.KeyCtrlS:
		m.saving = true
		return updateApplicationNotes(m.store, m.postingID, m.textarea.Value(), m.instance), nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd, nil
}

func (m *applicationNotesModel) View() string {
	var b strings.Builder
	b.WriteString(m.textarea.View() + "\n")
	if m.confirmingDiscard {
		b.WriteString(applicationNotesDiscardPrompt())
	} else {
		b.WriteString(applicationNotesHelp())
	}
	return b.String()
}
