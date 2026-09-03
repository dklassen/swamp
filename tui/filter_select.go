package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/filter"
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
		return filter.FieldDepartment, m.departmentOptions[m.cursor], true
	}
	idx := m.cursor - len(m.departmentOptions)
	if idx < len(m.locationOptions) {
		return filter.FieldLocation, m.locationOptions[idx], true
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
			case filter.FieldDepartment:
				m.selectedDepartments[value] = !m.selectedDepartments[value]
			case filter.FieldLocation:
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

// filterWindow splits a visibleWindow-style combined-cursor window across
// the department and location option groups, so a long combined list
// scrolls to keep the cursor visible the same way the company/posting
// lists already do (see visibleWindow). rows is the row budget for
// option lines only -- a group's section header renders "for free"
// whenever that group has at least one option in view, so it's never
// orphaned without its items, and hidden entirely once the window
// scrolls past it.
func filterWindow(cursor, deptCount, locCount, rows int) (deptStart, deptEnd, locStart, locEnd int) {
	start, end := visibleWindow(cursor, deptCount+locCount, rows)

	deptStart = start
	if deptStart > deptCount {
		deptStart = deptCount
	}
	deptEnd = end
	if deptEnd > deptCount {
		deptEnd = deptCount
	}

	locStart = start - deptCount
	if locStart < 0 {
		locStart = 0
	}
	locEnd = end - deptCount
	if locEnd < 0 {
		locEnd = 0
	}
	if locEnd > locCount {
		locEnd = locCount
	}
	return deptStart, deptEnd, locStart, locEnd
}

func (m *filterSelectModel) View(listRows int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Filters: %s", m.companyName)) + "\n")
	if len(m.departmentOptions) == 0 && len(m.locationOptions) == 0 {
		b.WriteString("No department/location values discovered yet -- refresh the company first.\n")
	}
	deptStart, deptEnd, locStart, locEnd := filterWindow(m.cursor, len(m.departmentOptions), len(m.locationOptions), listRows)
	if deptStart < deptEnd {
		b.WriteString(fieldLabel.Render("Department") + "\n")
		for i := deptStart; i < deptEnd; i++ {
			d := m.departmentOptions[i]
			b.WriteString(renderFilterOption(d, m.selectedDepartments[d], i == m.cursor))
		}
	}
	if locStart < locEnd {
		b.WriteString(fieldLabel.Render("Location") + "\n")
		for i := locStart; i < locEnd; i++ {
			l := m.locationOptions[i]
			b.WriteString(renderFilterOption(l, m.selectedLocations[l], len(m.departmentOptions)+i == m.cursor))
		}
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  space: toggle  enter: save  esc/b: cancel"))
	return b.String()
}
