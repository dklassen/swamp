package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
)

// mustIngestIn ingests a posting in department and location for companyID.
func mustIngestIn(t *testing.T, s testutil.TestApp, companyID int64, sourceID, department, location string) {
	t.Helper()
	if _, err := s.Postings.Ingest(t.Context(), companyID, jobboard.Ashby, sourceID, posting.IngestedFields{Title: sourceID, Department: department, Location: location}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
}

func TestFilterSelectModel_Init_ReadsTheCompanysOptions(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustIngestIn(t, s, acme.ID, "job-1", "Engineering", "Remote")
	m := newFilterSelectModel(s.Companies, message.FilterSelection{Company: companySnapshot(acme)}, message.ContentSize{})

	read, ok := m.Init()().(message.Event[message.Read[message.FilterOptions]])
	if !ok {
		t.Fatalf("Init sent %T, want a read of the filter options", m.Init()())
	}
	assertEventFrom(t, read.Meta, message.Source{Instance: m.Instance()})
	if want := (message.Subject{Type: message.TypeCompany, ID: acme.ID}); read.Meta.Subject != want {
		t.Errorf("Subject = %+v, want %+v", read.Meta.Subject, want)
	}
	want := message.FilterOptions{Departments: []string{"Engineering"}, Locations: []string{"Remote"}}
	if diff := cmp.Diff(want, read.Payload.Value); diff != "" {
		t.Errorf("options (-want +got):\n%s", diff)
	}
}

var testFilterCompany = message.Company{ID: 1, Name: "Acme"}

// pickerOf is a picker on companies opened on sel, once it has read
// Engineering and Sales, Remote and Onsite.
func pickerOf(companies *service.CompanyService, sel message.FilterSelection) *filterSelectModel {
	m := newFilterSelectModel(companies, sel, message.ContentSize{Rows: 40})
	m.optionsRead(message.New(m.source(), message.Subject{Type: message.TypeCompany, ID: sel.Company.ID}, message.OpNone, time.Now().UTC(),
		message.Read[message.FilterOptions]{Value: message.FilterOptions{Departments: []string{"Engineering", "Sales"}, Locations: []string{"Remote", "Onsite"}}}))
	return m
}

func TestFilterSelectModel_OpensWithTheNavsFiltersChecked(t *testing.T) {
	t.Parallel()

	m := pickerOf(nil, message.FilterSelection{Company: testFilterCompany, Departments: []string{"Sales"}, Locations: []string{"Onsite"}})

	want := message.FilterSelection{Company: testFilterCompany, Departments: []string{"Sales"}, Locations: []string{"Onsite"}}
	if diff := cmp.Diff(want, m.selection()); diff != "" {
		t.Errorf("selection (-want +got):\n%s", diff)
	}
}

func TestFilterSelectModel_CursorMovement_ClampsToTheOptions(t *testing.T) {
	t.Parallel()

	m := pickerOf(nil, message.FilterSelection{Company: testFilterCompany})
	for range 10 {
		m.keyPressed(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if last := len(m.departmentOptions) + len(m.locationOptions) - 1; m.cursor != last {
		t.Errorf("cursor after many downs = %d, want the last option, %d", m.cursor, last)
	}
	for range 10 {
		m.keyPressed(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if m.cursor != 0 {
		t.Errorf("cursor after many ups = %d, want 0", m.cursor)
	}
}

// The cursor runs through the departments, then the locations.
func TestFilterSelectModel_Space_TogglesTheOptionUnderTheCursor(t *testing.T) {
	t.Parallel()

	m := pickerOf(nil, message.FilterSelection{Company: testFilterCompany})
	space := tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	m.keyPressed(space)
	for range len(m.departmentOptions) {
		m.keyPressed(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	m.keyPressed(space)

	want := message.FilterSelection{Company: testFilterCompany, Departments: m.departmentOptions[:1], Locations: m.locationOptions[:1]}
	if diff := cmp.Diff(want, m.selection()); diff != "" {
		t.Errorf("selection after toggling the first of each (-want +got):\n%s", diff)
	}

	m.keyPressed(space)
	want.Locations = nil
	if diff := cmp.Diff(want, m.selection()); diff != "" {
		t.Errorf("selection after toggling the location again (-want +got):\n%s", diff)
	}
}

// statusIn is the status the first Notify in msgs reports, and whether
// there is one.
func statusIn(msgs []tea.Msg) (string, bool) {
	for _, msg := range msgs {
		if n, ok := msg.(message.Event[message.Notify]); ok {
			return n.Payload.Status, true
		}
	}
	return "", false
}

func TestFilterSelectModel_EscOrB_PopsAndClearsTheBanner(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('b')} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			m := pickerOf(nil, message.FilterSelection{Company: testFilterCompany})

			got := runCmd(t, m.keyPressed(key))
			if !popsIn(got, m.Instance()) {
				t.Errorf("%s sent %#v, want a pop of the picker", key, got)
			}
			if status, ok := statusIn(got); !ok || status != "" {
				t.Errorf("%s sent %#v, want an empty Notify", key, got)
			}
		})
	}
}

func TestFilterSelectModel_Enter_SavesTheSelectionAndReportsARan(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := companySnapshot(mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme"))
	m := pickerOf(s.Companies, message.FilterSelection{Company: acme, Departments: []string{"Sales"}, Locations: []string{"Remote"}})

	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}))

	if len(got) != 1 {
		t.Fatalf("enter sent %#v, want one Ran", got)
	}
	ran, ok := got[0].(message.Event[message.Ran[message.FilterSelection]])
	if !ok {
		t.Fatalf("enter sent %T, want a Ran of the selection", got[0])
	}
	assertEventFrom(t, ran.Meta, message.Source{Instance: m.Instance()})
	if want := (message.Subject{Type: message.TypeCompany, ID: acme.ID}); ran.Meta.Subject != want {
		t.Errorf("Subject = %+v, want %+v", ran.Meta.Subject, want)
	}
	want := message.FilterSelection{Company: acme, Departments: []string{"Sales"}, Locations: []string{"Remote"}}
	if diff := cmp.Diff(want, ran.Payload.Value); diff != "" {
		t.Errorf("Ran (-want +got):\n%s", diff)
	}
	saved, err := s.Companies.Filters(t.Context(), acme.ID)
	if err != nil {
		t.Fatalf("Filters: %v", err)
	}
	wantSaved := []filter.Filter{{Field: filter.FieldDepartment, Value: "Sales"}, {Field: filter.FieldLocation, Value: "Remote"}}
	if diff := cmp.Diff(wantSaved, saved); diff != "" {
		t.Errorf("saved filters (-want +got):\n%s", diff)
	}
}

func TestFilterSelectModel_Enter_SaveFails_ReportsAnErrorFromThePicker(t *testing.T) {
	t.Parallel()

	// A service on no database fails every write.
	m := pickerOf(service.NewCompanyService(store.Store{}), message.FilterSelection{Company: testFilterCompany, Departments: []string{"Sales"}})

	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}))

	if len(got) != 1 {
		t.Fatalf("enter sent %#v, want one Error", got)
	}
	failed, ok := got[0].(message.Event[message.Error])
	if !ok {
		t.Fatalf("enter sent %#v, want an Error", got[0])
	}
	assertEventFrom(t, failed.Meta, message.Source{Instance: m.Instance()})
	if want := (message.Subject{Type: message.TypeCompany, ID: testFilterCompany.ID}); failed.Meta.Subject != want || failed.Meta.Op != message.OpUpdated {
		t.Errorf("Subject, Op = %+v, %v, want %+v, %v", failed.Meta.Subject, failed.Meta.Op, want, message.OpUpdated)
	}
}

func TestFilterSelectModel_Saved_PopsAndSaysWhoseFilters(t *testing.T) {
	t.Parallel()

	m := pickerOf(nil, message.FilterSelection{Company: testFilterCompany})
	sel := m.selection()

	got := runCmd(t, m.saved(message.New(m.source(), message.Subject{Type: message.TypeCompany, ID: testFilterCompany.ID}, message.OpNone, time.Now().UTC(),
		message.Ran[message.FilterSelection]{Value: sel})))

	if !popsIn(got, m.Instance()) {
		t.Errorf("saved sent %#v, want a pop of the picker", got)
	}
	if status, _ := statusIn(got); status != testFilterCompany.Name+" filters saved" {
		t.Errorf("status = %q, want %q", status, testFilterCompany.Name+" filters saved")
	}
}

func TestFilterSelectModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, newFilterSelectModel(nil, message.FilterSelection{Company: testFilterCompany}, message.ContentSize{}))
}

func TestFilterWindow(t *testing.T) {
	tests := []struct {
		name                                                 string
		cursor, numDepts, numLocs, rows                      int
		wantDeptStart, wantDeptEnd, wantLocStart, wantLocEnd int
	}{
		{
			name:          "fits everything returns full range for both groups",
			cursor:        1,
			numDepts:      2,
			numLocs:       2,
			rows:          10,
			wantDeptStart: 0, wantDeptEnd: 2,
			wantLocStart: 0, wantLocEnd: 2,
		},
		{
			// Before the first tea.WindowSizeMsg arrives, height is 0 --
			// don't hide anything (mirrors visibleWindow's own
			// zero-rows behavior).
			name:          "zero rows returns full range for both groups",
			cursor:        4,
			numDepts:      2,
			numLocs:       3,
			rows:          0,
			wantDeptStart: 0, wantDeptEnd: 2,
			wantLocStart: 0, wantLocEnd: 3,
		},
		{
			// cursor 10 = location index 8 (10 - 2 departments) of 20
			// locations -- window has scrolled past all departments
			// (empty dept range), 5 rows centered on location index 8.
			name:          "cursor deep in locations hides department group entirely",
			cursor:        10,
			numDepts:      2,
			numLocs:       20,
			rows:          5,
			wantDeptStart: 2, wantDeptEnd: 2,
			wantLocStart: 6, wantLocEnd: 11,
		},
		{
			// Window hasn't reached locations yet (empty loc range); 5
			// rows starting at the top of 20 departments.
			name:          "cursor in departments hides location group entirely",
			cursor:        1,
			numDepts:      20,
			numLocs:       5,
			rows:          5,
			wantDeptStart: 0, wantDeptEnd: 5,
			wantLocStart: 0, wantLocEnd: 0,
		},
		{
			// cursor 2 = last department (index 2) of 3 departments + 3
			// locations, 4 rows -- window should show all 3 departments
			// and the first location, keeping the cursor visible
			// without hiding either header unnecessarily.
			name:          "window straddles both groups shows part of each",
			cursor:        2,
			numDepts:      3,
			numLocs:       3,
			rows:          4,
			wantDeptStart: 0, wantDeptEnd: 3,
			wantLocStart: 0, wantLocEnd: 1,
		},
		{
			name:          "no departments treats group as empty",
			cursor:        2,
			numDepts:      0,
			numLocs:       5,
			rows:          3,
			wantDeptStart: 0, wantDeptEnd: 0,
			wantLocStart: 1, wantLocEnd: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deptStart, deptEnd, locStart, locEnd := filterWindow(tt.cursor, tt.numDepts, tt.numLocs, tt.rows)
			if deptStart != tt.wantDeptStart || deptEnd != tt.wantDeptEnd {
				t.Errorf("dept range = (%d, %d), want (%d, %d)", deptStart, deptEnd, tt.wantDeptStart, tt.wantDeptEnd)
			}
			if locStart != tt.wantLocStart || locEnd != tt.wantLocEnd {
				t.Errorf("loc range = (%d, %d), want (%d, %d)", locStart, locEnd, tt.wantLocStart, tt.wantLocEnd)
			}
		})
	}
}
