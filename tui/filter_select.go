package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// filterSelectModel picks the departments and locations one company's
// posting list keeps to, and saves them.
type filterSelectModel struct {
	companies *service.CompanyService
	company   message.Company

	departmentOptions []string
	locationOptions   []string
	cursor            int

	selectedDepartments map[string]bool
	selectedLocations   map[string]bool

	focused  bool
	height   int
	instance message.ScreenInstance
}

// newFilterSelectModel opens on sel, the company's filters as its list has
// them. The options come with Init.
func newFilterSelectModel(companies *service.CompanyService, sel message.FilterSelection, size message.ContentSize) *filterSelectModel {
	return &filterSelectModel{
		companies:           companies,
		company:             sel.Company,
		selectedDepartments: toSet(sel.Departments),
		selectedLocations:   toSet(sel.Locations),
		height:              size.Rows,
		instance:            message.NewScreenInstance(),
	}
}

func (m *filterSelectModel) source() message.Source {
	return message.Source{Instance: m.instance}
}

func (m *filterSelectModel) Instance() message.ScreenInstance { return m.instance }

func (m *filterSelectModel) Focused() bool { return m.focused }

func (m *filterSelectModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.optionsRead, model.From(m.instance)),
		model.Always(m.saved, model.From(m.instance)),
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *filterSelectModel) Init() tea.Cmd {
	return loadFilterOptions(m.companies, m.source(), m.company.ID)
}

// Update has nothing to do: everything the picker handles is registered.
func (m *filterSelectModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *filterSelectModel) optionsRead(e message.Event[message.Read[message.FilterOptions]]) tea.Cmd {
	m.departmentOptions, m.locationOptions = e.Payload.Value.Departments, e.Payload.Value.Locations
	return nil
}

// saved closes the picker. A failed save sends an Error instead, which
// leaves it open with the selection as made.
func (m *filterSelectModel) saved(message.Event[message.Ran[message.FilterSelection]]) tea.Cmd {
	return m.close(m.company.Name + " filters saved")
}

func (m *filterSelectModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.height = e.Payload.Value.Rows
	return nil
}

func (m *filterSelectModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

// close takes a failed save's error from the banner with the picker.
func (m *filterSelectModel) close(status string) tea.Cmd {
	return tea.Batch(message.Pop(m.source()), message.ReportStatus(m.source(), status))
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

// selection is the company with the currently checked department and
// location values, in the same order as departmentOptions/locationOptions.
func (m *filterSelectModel) selection() message.FilterSelection {
	sel := message.FilterSelection{Company: m.company}
	for _, d := range m.departmentOptions {
		if m.selectedDepartments[d] {
			sel.Departments = append(sel.Departments, d)
		}
	}
	for _, l := range m.locationOptions {
		if m.selectedLocations[l] {
			sel.Locations = append(sel.Locations, l)
		}
	}
	return sel
}

func (m *filterSelectModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	total := len(m.departmentOptions) + len(m.locationOptions)
	switch msg.String() {
	case "down", "j":
		if m.cursor < total-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "space":
		field, value, ok := m.itemAtCursor()
		if ok {
			switch field {
			case filter.FieldDepartment:
				m.selectedDepartments[value] = !m.selectedDepartments[value]
			case filter.FieldLocation:
				m.selectedLocations[value] = !m.selectedLocations[value]
			}
		}
	case "esc", "b":
		return m.close("")
	case "enter":
		return saveFilters(m.companies, m.source(), m.selection())
	}
	return nil
}

func loadFilterOptions(companies *service.CompanyService, src message.Source, companyID int64) tea.Cmd {
	subject := message.Subject{Type: message.TypeCompany, ID: companyID}
	return func() tea.Msg {
		ctx := context.Background()
		var options message.FilterOptions
		var err error
		if options.Departments, err = companies.Departments(ctx, companyID); err != nil {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		if options.Locations, err = companies.Locations(ctx, companyID); err != nil {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.FilterOptions]{Value: options})
	}
}

// saveFilters reports a Ran rather than a Changed, since the filters
// aren't an entity other screens show; the posting list hears it about its
// company.
func saveFilters(companies *service.CompanyService, src message.Source, sel message.FilterSelection) tea.Cmd {
	subject := message.Subject{Type: message.TypeCompany, ID: sel.Company.ID}
	return func() tea.Msg {
		if err := companies.SetFilters(context.Background(), sel.Company.ID, companyFiltersFrom(sel.Departments, sel.Locations)); err != nil {
			return message.New(src, subject, message.OpUpdated, time.Now().UTC(), model.ErrorOf(err))
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Ran[message.FilterSelection]{Value: sel})
	}
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

// View renders the options in the rows of the last content size: what
// the help line leaves, less a row for each section label in view.
func (m *filterSelectModel) View() tea.View {
	var b strings.Builder
	help := model.HelpStyle.Render("↑/↓ (j/k): select  space: toggle  enter: save  esc/b: cancel")
	if len(m.departmentOptions) == 0 && len(m.locationOptions) == 0 {
		b.WriteString("No department/location values discovered yet -- refresh the company first.\n")
	}
	rows := m.height - lipgloss.Height(help)
	// Which labels show depends on the window, so allow for one, and only
	// if that window spans both sections, make room for the second.
	deptStart, deptEnd, locStart, locEnd := filterWindow(m.cursor, len(m.departmentOptions), len(m.locationOptions), rows-1)
	if deptStart < deptEnd && locStart < locEnd {
		deptStart, deptEnd, locStart, locEnd = filterWindow(m.cursor, len(m.departmentOptions), len(m.locationOptions), rows-2)
	}
	if deptStart < deptEnd {
		b.WriteString(model.FieldLabel.Render("Department") + "\n")
		for i := deptStart; i < deptEnd; i++ {
			d := m.departmentOptions[i]
			b.WriteString(renderFilterOption(d, m.selectedDepartments[d], i == m.cursor))
		}
	}
	if locStart < locEnd {
		b.WriteString(model.FieldLabel.Render("Location") + "\n")
		for i := locStart; i < locEnd; i++ {
			l := m.locationOptions[i]
			b.WriteString(renderFilterOption(l, m.selectedLocations[l], len(m.departmentOptions)+i == m.cursor))
		}
	}
	b.WriteString(help)
	return tea.NewView(b.String())
}
