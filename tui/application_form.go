package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// applicationFormModel drives the screen for entering a posting's
// application form by hand (#184): whether each document is required,
// optional or absent, and the custom questions, as the text
// jobboard.ParseFormText reads. It's for boards whose forms Swamp can't
// fetch (Ashby, Lever); stage_prepare returns what's saved here the same
// way it returns a fetched Greenhouse form.
type applicationFormModel struct {
	store     *store.Store
	postingID int64
	// previous is the stored form being edited, if any, so questions the
	// user didn't change keep the Type and Options the text doesn't show.
	previous *jobboard.ApplicationForm
	textarea textarea.Model
	// parseErr is why the last ctrl+s didn't save. The screen stays open
	// so the user can fix the line it names.
	parseErr error
}

func newApplicationFormModel(s *store.Store, postingID int64, previous *jobboard.ApplicationForm, width, height int) applicationFormModel {
	ta := textarea.New()
	ta.SetWidth(width)
	seed := jobboard.ApplicationForm{}
	if previous != nil {
		seed = *previous
	}
	ta.SetValue(jobboard.FormText(seed))
	ta.Focus()
	m := applicationFormModel{store: s, postingID: postingID, previous: previous, textarea: ta}
	m.setHeight(height)
	return m
}

// setHeight fits the screen into height rows, giving the text area what
// the title, error and help lines leave (issue #138).
func (m *applicationFormModel) setHeight(height int) {
	m.textarea.SetHeight(max(height-lipgloss.Height(applicationFormTitle())-lipgloss.Height(applicationFormHelp())-1, 0))
}

func applicationFormTitle() string {
	return titleStyle.Render("Application form") + "\n" + dimStyle.Render("What the apply page asks for. Paste its questions below.")
}

func applicationFormHelp() string { return helpStyle.Render("ctrl+s: save  esc: cancel") }

// cancelApplicationFormMsg signals that App should go back to application
// detail without saving.
type cancelApplicationFormMsg struct{}

func (m *applicationFormModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch msg.Type {
	case tea.KeyEsc:
		return nil, cancelApplicationFormMsg{}
	case tea.KeyCtrlS:
		form, err := jobboard.ParseFormText(m.textarea.Value(), m.previous)
		m.parseErr = err
		if err != nil {
			return nil, nil
		}
		return saveApplicationForm(m.store, m.postingID, form), nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd, nil
}

func (m *applicationFormModel) View() string {
	var b strings.Builder
	b.WriteString(applicationFormTitle() + "\n")
	if m.parseErr != nil {
		b.WriteString(errStyle.Render(m.parseErr.Error()))
	}
	b.WriteString("\n")
	b.WriteString(m.textarea.View() + "\n")
	b.WriteString(applicationFormHelp())
	return b.String()
}

// enterApplicationFormMsg is application detail asking App to open the
// form screen for postingID, once its stored form is loaded.
type enterApplicationFormMsg struct {
	postingID int64
}

type applicationFormLoadedMsg struct {
	postingID int64
	form      *jobboard.ApplicationForm // nil: none stored yet
	err       error
}

// loadApplicationForm reads postingID's stored form, fetched or entered by
// hand, to seed the form screen.
func loadApplicationForm(s *store.Store, postingID int64) tea.Cmd {
	return func() tea.Msg {
		stored, _, ok, err := s.GetApplicationForm(context.Background(), postingID)
		if err != nil || !ok {
			return applicationFormLoadedMsg{postingID: postingID, err: err}
		}
		var form jobboard.ApplicationForm
		if err := json.Unmarshal([]byte(stored), &form); err != nil {
			return applicationFormLoadedMsg{postingID: postingID, err: fmt.Errorf("tui: decode stored application form: %w", err)}
		}
		return applicationFormLoadedMsg{postingID: postingID, form: &form}
	}
}

type applicationFormSavedMsg struct {
	err error
}

// saveApplicationForm stores form as postingID's application form, where
// stage_prepare reads it (#168, #184).
func saveApplicationForm(s *store.Store, postingID int64, form jobboard.ApplicationForm) tea.Cmd {
	return func() tea.Msg {
		encoded, err := json.Marshal(form)
		if err != nil {
			return applicationFormSavedMsg{err: fmt.Errorf("tui: encode application form: %w", err)}
		}
		return applicationFormSavedMsg{err: s.SaveApplicationForm(context.Background(), postingID, string(encoded))}
	}
}
