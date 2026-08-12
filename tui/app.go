// Package tui is the terminal UI: Bubble Tea models for browsing and
// managing companies (and, in later increments, their postings). Update
// stays side-effect-free -- store calls are wrapped in tea.Cmd so the
// message-passing logic is testable without a real terminal; View
// (rendering) is not held to automated test coverage, per this project's
// testing decisions.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dklassen/swamp/store"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).MarginBottom(1)
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).MarginTop(1)
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	fieldLabel   = lipgloss.NewStyle().Bold(true)
	focusedLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
)

type screen int

const (
	screenCompanyList screen = iota
	screenCompanyForm
)

// formInputs indices: name, then source ref. Source is fixed to "ashby"
// for now (the only supported job board), so there's no source picker.
const (
	formFieldName = iota
	formFieldSourceRef
	formFieldCount
)

type App struct {
	store      *store.Store
	companies  []store.Company
	cursor     int
	screen     screen
	formInputs []textinput.Model
	formFocus  int
	err        error
}

func New(s *store.Store) *App {
	inputs := make([]textinput.Model, formFieldCount)
	for i := range inputs {
		inputs[i] = textinput.New()
	}
	return &App{store: s, formInputs: inputs}
}

type companiesLoadedMsg struct {
	companies []store.Company
	err       error
}

func loadCompanies(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		companies, err := s.ListActiveCompanies(context.Background())
		return companiesLoadedMsg{companies: companies, err: err}
	}
}

func (a *App) Init() tea.Cmd {
	return loadCompanies(a.store)
}

type companyCreatedMsg struct {
	company store.Company
	err     error
}

func createCompany(s *store.Store, name, sourceRef string) tea.Cmd {
	return func() tea.Msg {
		company, err := s.CreateCompany(context.Background(), name, "ashby", sourceRef)
		return companyCreatedMsg{company: company, err: err}
	}
}

type companyPausedMsg struct {
	companyID int64
	err       error
}

func pauseCompany(s *store.Store, companyID int64) tea.Cmd {
	return func() tea.Msg {
		err := s.SoftDeleteCompany(context.Background(), companyID)
		return companyPausedMsg{companyID: companyID, err: err}
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case companiesLoadedMsg:
		a.err = msg.err
		a.companies = msg.companies
	case companyCreatedMsg:
		a.err = msg.err
		if msg.err == nil {
			a.companies = append(a.companies, msg.company)
			a.screen = screenCompanyList
		}
	case companyPausedMsg:
		a.err = msg.err
		if msg.err == nil {
			for i, c := range a.companies {
				if c.ID == msg.companyID {
					a.companies = append(a.companies[:i], a.companies[i+1:]...)
					break
				}
			}
			if a.cursor >= len(a.companies) && a.cursor > 0 {
				a.cursor = len(a.companies) - 1
			}
		}
	case tea.KeyMsg:
		switch a.screen {
		case screenCompanyList:
			switch {
			case msg.Type == tea.KeyDown:
				if a.cursor < len(a.companies)-1 {
					a.cursor++
				}
			case msg.Type == tea.KeyUp:
				if a.cursor > 0 {
					a.cursor--
				}
			case msg.String() == "q":
				return a, tea.Quit
			case msg.String() == "p":
				if a.cursor < len(a.companies) {
					return a, pauseCompany(a.store, a.companies[a.cursor].ID)
				}
			case msg.String() == "a":
				a.screen = screenCompanyForm
				a.formFocus = 0
				for i := range a.formInputs {
					a.formInputs[i].SetValue("")
					a.formInputs[i].Blur()
				}
				a.formInputs[0].Focus()
			}
		case screenCompanyForm:
			if msg.Type == tea.KeyEsc {
				a.screen = screenCompanyList
				return a, nil
			}
			if msg.Type == tea.KeyTab {
				a.formInputs[a.formFocus].Blur()
				a.formFocus = (a.formFocus + 1) % formFieldCount
				a.formInputs[a.formFocus].Focus()
				return a, nil
			}
			if msg.Type == tea.KeyEnter {
				name := a.formInputs[formFieldName].Value()
				sourceRef := a.formInputs[formFieldSourceRef].Value()
				if name == "" || sourceRef == "" {
					return a, nil
				}
				return a, createCompany(a.store, name, sourceRef)
			}
			var cmd tea.Cmd
			a.formInputs[a.formFocus], cmd = a.formInputs[a.formFocus].Update(msg)
			return a, cmd
		}
	}
	return a, nil
}

func (a *App) View() string {
	var b strings.Builder

	if a.err != nil {
		b.WriteString(errStyle.Render(fmt.Sprintf("error: %v", a.err)) + "\n\n")
	}

	switch a.screen {
	case screenCompanyForm:
		b.WriteString(titleStyle.Render("Add company") + "\n")
		labels := []string{"Name", "Ashby slug"}
		for i, input := range a.formInputs {
			label := fieldLabel
			if i == a.formFocus {
				label = focusedLabel
			}
			b.WriteString(label.Render(labels[i]+":") + " " + input.View() + "\n")
		}
		b.WriteString(helpStyle.Render("tab: next field  enter: save  esc: cancel"))
	default:
		b.WriteString(titleStyle.Render("Companies") + "\n")
		if len(a.companies) == 0 {
			b.WriteString("No companies yet. Press 'a' to add one.\n")
		}
		for i, c := range a.companies {
			line := fmt.Sprintf("%s (%s)", c.Name, c.SourceRef)
			if i == a.cursor {
				b.WriteString(cursorStyle.Render("> "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		b.WriteString(helpStyle.Render("↑/↓: select  a: add  p: pause  q: quit"))
	}

	return b.String()
}
