package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

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
}

// newApplicationNotesModel returns a notes-edit screen for postingID,
// seeded with notes and sized to width/height (the terminal geometry at
// construction time -- there's no live resize handling for this screen,
// matching the pre-extraction behavior).
func newApplicationNotesModel(s *store.Store, postingID int64, notes string, width, height int) applicationNotesModel {
	ta := textarea.New()
	ta.SetWidth(width)
	ta.SetHeight(height)
	ta.SetValue(notes)
	ta.Focus()
	return applicationNotesModel{store: s, postingID: postingID, textarea: ta}
}

// cancelApplicationNotesMsg signals that App should switch back to the
// posting-detail screen without saving.
type cancelApplicationNotesMsg struct{}

func (m *applicationNotesModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch msg.Type {
	case tea.KeyEsc:
		return nil, cancelApplicationNotesMsg{}
	case tea.KeyCtrlS:
		return updateApplicationNotes(m.store, m.postingID, m.textarea.Value()), nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd, nil
}

func (m *applicationNotesModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Edit application notes") + "\n")
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(helpStyle.Render("ctrl+s: save  esc: cancel"))
	return b.String()
}
