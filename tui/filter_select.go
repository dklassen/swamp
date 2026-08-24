package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

// filterSelectModel drives the department/location filter-select screen
// for a single company. It holds the store it needs to save filters, the
// company it's editing (fixed for this screen's lifetime), and its own
// private selection state -- no other screen reads or writes this.
type filterSelectModel struct {
	store       *store.Store
	companyID   int64
	companyName string

	departmentOptions []string
	locationOptions   []string
	cursor            int

	selectedDepartments map[string]bool
	selectedLocations   map[string]bool
}

// newFilterSelectModel returns a filter-select screen for companyID,
// seeded from the options and existing saved filters loaded by
// loadFilterOptions -- see filterOptionsLoadedMsg in App.Update, the one
// place this is constructed (filter options are loaded async, so this
// can't be seeded synchronously the way a key-press-triggered screen
// entry can).
func newFilterSelectModel(s *store.Store, companyID int64, companyName string, departments, locations []string, existingFilters []store.CompanyFilter) filterSelectModel {
	existingDepartments, existingLocations := splitCompanyFilters(existingFilters)
	return filterSelectModel{
		store:               s,
		companyID:           companyID,
		companyName:         companyName,
		departmentOptions:   departments,
		locationOptions:     locations,
		selectedDepartments: toSet(existingDepartments),
		selectedLocations:   toSet(existingLocations),
	}
}

// itemAtCursor maps cursor into the combined departments-then-locations
// list, returning which field/value it points at. ok is false if the
// cursor is out of range (e.g. no options loaded yet).
func (m *filterSelectModel) itemAtCursor() (field, value string, ok bool) {
	if m.cursor < len(m.departmentOptions) {
		return "department", m.departmentOptions[m.cursor], true
	}
	idx := m.cursor - len(m.departmentOptions)
	if idx < len(m.locationOptions) {
		return "location", m.locationOptions[idx], true
	}
	return "", "", false
}

// selectedValues returns the currently checked department and location
// values, in the same order as departmentOptions/locationOptions.
func (m *filterSelectModel) selectedValues() (departments, locations []string) {
	for _, d := range m.departmentOptions {
		if m.selectedDepartments[d] {
			departments = append(departments, d)
		}
	}
	for _, l := range m.locationOptions {
		if m.selectedLocations[l] {
			locations = append(locations, l)
		}
	}
	return departments, locations
}

// cancelFilterSelectMsg signals that App should switch back to the
// posting-list screen without saving.
type cancelFilterSelectMsg struct{}

func (m *filterSelectModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	total := len(m.departmentOptions) + len(m.locationOptions)
	switch {
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < total-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.Type == tea.KeySpace:
		field, value, ok := m.itemAtCursor()
		if ok {
			switch field {
			case "department":
				m.selectedDepartments[value] = !m.selectedDepartments[value]
			case "location":
				m.selectedLocations[value] = !m.selectedLocations[value]
			}
		}
	case msg.Type == tea.KeyEsc, msg.String() == "b":
		return nil, cancelFilterSelectMsg{}
	case msg.Type == tea.KeyEnter:
		departments, locations := m.selectedValues()
		return saveCompanyFilters(m.store, m.companyID, departments, locations), nil
	}
	return nil, nil
}

func (m *filterSelectModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Filters: %s", m.companyName)) + "\n")
	if len(m.departmentOptions) == 0 && len(m.locationOptions) == 0 {
		b.WriteString("No department/location values discovered yet -- refresh the company first.\n")
	}
	idx := 0
	if len(m.departmentOptions) > 0 {
		b.WriteString(fieldLabel.Render("Department") + "\n")
		for _, d := range m.departmentOptions {
			b.WriteString(renderFilterOption(d, m.selectedDepartments[d], idx == m.cursor))
			idx++
		}
	}
	if len(m.locationOptions) > 0 {
		b.WriteString(fieldLabel.Render("Location") + "\n")
		for _, l := range m.locationOptions {
			b.WriteString(renderFilterOption(l, m.selectedLocations[l], idx == m.cursor))
			idx++
		}
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  space: toggle  enter: save  esc/b: cancel"))
	return b.String()
}
