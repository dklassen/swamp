package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// companyListOf is a company list that has loaded companies and drawn in
// width by rows.
func companyListOf(t *testing.T, companies []company.Company, openPostings map[int64]int, width, rows int) *companyListModel {
	t.Helper()
	m := newCompanyListModel(nil, nil)
	m.companiesRead(companiesReadOf(companies, openPostings))
	m.resize(message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(),
		message.Signal[message.ContentSize]{Value: message.ContentSize{Width: width, Rows: rows}}))
	return m
}

// companiesReadOf is the read of companies, with openPostings open
// postings each.
func companiesReadOf(companies []company.Company, openPostings map[int64]int) message.Event[message.Read[message.CompanyList]] {
	list := message.CompanyList{OpenPostings: openPostings}
	for _, c := range companies {
		list.Companies = append(list.Companies, companySnapshot(c))
	}
	return message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Read[message.CompanyList]{Value: list})
}

// companiesLoadFailed is what loading the companies sends when it fails
// with err.
func companiesLoadFailed(err error) message.Event[message.Error] {
	return message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
}

// press sends key to m and returns the message its command produces, or
// nil when it returns none.
func press(t *testing.T, m *companyListModel, key tea.KeyPressMsg) tea.Msg {
	t.Helper()
	cmd := m.keyPressed(key)
	if cmd == nil {
		return nil
	}
	return cmd()
}

// focusOn is the signal App sends when instance comes into focus.
func focusOn(instance message.ScreenInstance) message.Event[message.Signal[message.FocusChanged]] {
	return message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(),
		message.Signal[message.FocusChanged]{Value: message.FocusChanged{Focused: instance}})
}

func companyListView(m *companyListModel) string {
	return m.View().Content
}

func TestCompanyListModel_Init_ReadsTheCompaniesAndTheirOpenPostings(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	globex := mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	m := newCompanyListModel(s.Companies, s.Postings)

	msg := m.Init()()

	got, ok := msg.(message.Event[message.Read[message.CompanyList]])
	if !ok {
		t.Fatalf("Init sent %T, want message.Event[message.Read[message.CompanyList]]", msg)
	}
	assertEventFrom(t, got.Meta, message.Source{Instance: m.Instance()})
	want := message.CompanyList{
		Companies:    []message.Company{companySnapshot(acme), companySnapshot(globex)},
		OpenPostings: map[int64]int{acme.ID: 1},
	}
	if diff := cmp.Diff(want, got.Payload.Value); diff != "" {
		t.Errorf("companies read (-want +got):\n%s", diff)
	}
}

func TestCompanyListModel_LoadRequest_SendsItsRead(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	m := newCompanyListModel(s.Companies, s.Postings)
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cmd := r.Dispatch(message.New(message.Source{Instance: message.NewScreenInstance()}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[message.CompanyList]{}))
	if cmd == nil {
		t.Fatal("a load request sent nothing, want the companies read")
	}

	got, ok := cmd().(message.Event[message.Read[message.CompanyList]])
	if !ok {
		t.Fatalf("a load request sent %T, want message.Event[message.Read[message.CompanyList]]", got)
	}
	assertEventFrom(t, got.Meta, message.Source{Instance: m.Instance()})
	if diff := cmp.Diff([]message.Company{companySnapshot(acme)}, got.Payload.Value.Companies); diff != "" {
		t.Errorf("companies read (-want +got):\n%s", diff)
	}
}

func TestCompanyListModel_CompanyChanged_KeepsTheRowsInStep(t *testing.T) {
	t.Parallel()

	acme := company.Company{ID: 1, Name: "Acme"}
	globex := company.Company{ID: 2, Name: "Globex"}
	src := message.Source{Instance: message.NewScreenInstance()}
	at := time.Now().UTC()
	tests := []struct {
		name  string
		event message.Event[message.Changed[message.Company]]
		want  []string
	}{
		{name: "deleted", event: message.Deleted(src, acme.ID, at, companySnapshot(acme)), want: []string{"Globex"}},
		{name: "created", event: message.Created(src, 3, at, message.Company{ID: 3, Name: "Bluth"}), want: []string{"Acme", "Bluth", "Globex"}},
		{name: "renamed", event: message.Updated(src, acme.ID, at, companySnapshot(acme), message.Company{ID: acme.ID, Name: "Zeta"}), want: []string{"Globex", "Zeta"}},
		{name: "deleted, not shown", event: message.Deleted(src, 3, at, message.Company{ID: 3, Name: "Initech"}), want: []string{"Acme", "Globex"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := companyListOf(t, []company.Company{acme, globex}, nil, 80, 20)

			m.companyChanged(tt.event)

			var got []string
			for _, c := range m.companies {
				got = append(got, c.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("companies after the event (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCompanyListModel_CursorMovement(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}, nil, 0, 40)

	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", m.cursor)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor after down at bottom = %d, want 1 (clamped)", m.cursor)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", m.cursor)
	}
}

func TestCompanyListModel_Enter_ReturnsOpenCompanyPostingsMsg(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}}, nil, 0, 40)

	intent := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	sel, ok := intent.(message.OpenCompanyPostingsMsg)
	if !ok {
		t.Fatalf("intent = %T, want message.OpenCompanyPostingsMsg", intent)
	}
	if sel.Company.ID != 1 {
		t.Fatalf("message.OpenCompanyPostingsMsg.Company.ID = %d, want 1", sel.Company.ID)
	}
}

func TestCompanyListModel_Enter_NoCompanies_ReturnsNothing(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, nil, nil, 0, 40)

	if intent := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}); intent != nil {
		t.Fatalf("intent = %#v, want nil", intent)
	}
}

func TestCompanyListModel_E_ReturnsOpenCompanyEditMsg(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}}, nil, 0, 40)

	intent := press(t, m, runeKey('e'))
	edit, ok := intent.(message.OpenCompanyEditMsg)
	if !ok {
		t.Fatalf("intent = %T, want message.OpenCompanyEditMsg", intent)
	}
	if edit.Company.ID != 1 {
		t.Fatalf("message.OpenCompanyEditMsg.Company.ID = %d, want 1", edit.Company.ID)
	}
}

// TestCompanyListModel_R_AsksToSyncTheCompany: 'r' asks for the selected
// company to be refreshed rather than starting the sync itself, so what
// knows about any other sync running decides (#152).
func TestCompanyListModel_R_AsksToSyncTheCompany(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}, nil, 0, 40)
	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	intent := press(t, m, runeKey('r'))
	refresh, ok := intent.(message.Event[message.Run[message.SyncCompany]])
	if !ok {
		t.Fatalf("intent = %T, want message.Event[message.Run[message.SyncCompany]]", intent)
	}
	if want := (message.Subject{Type: message.TypeCompany, ID: 2}); refresh.Meta.Subject != want || refresh.Payload.Value.Company.Name != "Globex" {
		t.Fatalf("refresh = %+v, want Globex with subject %+v", refresh, want)
	}
}

func TestCompanyListModel_D_ReturnsOpenCompanyDeleteMsgWithOpenPostings(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}}, map[int64]int{1: 3}, 0, 40)

	intent := press(t, m, runeKey('d'))
	del, ok := intent.(message.OpenCompanyDeleteMsg)
	if !ok {
		t.Fatalf("intent = %T, want message.OpenCompanyDeleteMsg", intent)
	}
	if del.Company.ID != 1 || del.OpenPostings != 3 {
		t.Fatalf("message.OpenCompanyDeleteMsg = %+v, want company 1 with 3 open postings", del)
	}
}

// A command runs off the event loop, so it carries the company the cursor
// was on when the key was pressed, not wherever the cursor is later.
func TestCompanyListModel_CommandKeepsTheCompanyFromWhenTheKeyWasPressed(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}, nil, 0, 40)

	cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})
	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	if sel, ok := cmd().(message.OpenCompanyPostingsMsg); !ok || sel.Company.ID != 1 {
		t.Fatalf("enter's command after the cursor moved = %#v, want message.OpenCompanyPostingsMsg for Acme (ID 1)", sel)
	}
}

// 'R' hands the run its own copy of the companies, so a reload landing
// mid-run can't change which companies it syncs.
func TestCompanyListModel_ShiftR_AsksToSyncACopyOfTheCompanies(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}, nil, 0, 40)

	intent := press(t, m, runeKey('R'))
	start, ok := intent.(message.Event[message.Run[message.SyncAll]])
	if !ok {
		t.Fatalf("intent = %T, want message.Event[message.Run[message.SyncAll]]", intent)
	}
	m.companies[0].Name = "Changed"
	if got := start.Payload.Value.Companies; len(got) != 2 || got[0].Name != "Acme" || got[1].Name != "Globex" {
		t.Fatalf("SyncAll.Companies = %+v, want a copy of Acme and Globex", got)
	}
}

func TestCompanyListModel_R_NoCompanies_ReturnsNothing(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, nil, nil, 0, 40)

	if intent := press(t, m, runeKey('r')); intent != nil {
		t.Fatalf("intent = %#v, want nil", intent)
	}
}

func TestCompanyListModel_E_NoCompanies_ReturnsNothing(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, nil, nil, 0, 40)

	if intent := press(t, m, runeKey('e')); intent != nil {
		t.Fatalf("intent = %#v, want nil", intent)
	}
}

// The company list is a tab, so there's nothing to go back to; a stray
// esc mustn't leave it.
func TestCompanyListModel_EscOrB_DoesNothing(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('b')} {
		m := companyListOf(t, nil, nil, 0, 40)
		if intent := press(t, m, key); intent != nil {
			t.Errorf("press(%v) = %#v, want nil", key, intent)
		}
	}
}

func TestCompanyListModel_A_ReturnsOpenCompanyFormMsg(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, nil, nil, 0, 40)

	if intent := press(t, m, runeKey('a')); intent != (message.OpenCompanyFormMsg{}) {
		t.Fatalf("intent = %T, want message.OpenCompanyFormMsg", intent)
	}
}

// assertHandlersFailOnWrongMessage checks every one of c's routes sends
// a fault when handed a message of another type, rather than panicking.
func assertHandlersFailOnWrongMessage(t *testing.T, c model.Routable) {
	t.Helper()
	if err := model.NewMsgRouter().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, route := range c.Routes() {
		t.Run(route.ID().String(), func(t *testing.T) {
			cmd := route.Handle(probeMsg{})
			if cmd == nil {
				t.Fatal("handler given probeMsg returned no command, want one sending a fault")
			}
			if got, ok := cmd().(message.Event[message.Error]); !ok || got.Payload.Code != message.ErrFault || got.Payload.Text == "" {
				t.Errorf("handler given probeMsg sent %#v, want an Event[Error] with code %v and a Text", got, message.ErrFault)
			}
		})
	}
}

func TestCompanyListModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, newCompanyListModel(nil, nil))
}

func TestCompanyListModel_ClampCursor(t *testing.T) {
	t.Parallel()

	m := &companyListModel{cursor: 2}
	m.clampCursor(1)
	if m.cursor != 0 {
		t.Fatalf("cursor after clamp = %d, want 0", m.cursor)
	}
}

func TestCompanyListModel_View_ShowsOpenPostingsAndLastFetched(t *testing.T) {
	t.Parallel()

	fetched := time.Date(2026, 9, 24, 0, 52, 0, 0, time.UTC)
	companies := []company.Company{
		{ID: 1, Name: "Contoso", Source: jobboard.Greenhouse, SourceRef: "contoso", LastFetchedAt: fetched},
		{ID: 2, Name: "Cyberdyne", Source: jobboard.Ashby, SourceRef: "cyberdyne-ai"},
	}
	openPostings := map[int64]int{1: 14}

	got := companyListView(companyListOf(t, companies, openPostings, 0, 40))

	for _, want := range []string{
		"Contoso", "14", fetched.Local().Format("2006-01-02 15:04"),
		"Cyberdyne", "never",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("View missing %q:\n%s", want, got)
		}
	}
	// Board and slug are left out of the list on purpose: they don't help
	// decide where to look next.
	for _, notWant := range []string{jobboard.Greenhouse, "cyberdyne-ai", "Board", "Slug"} {
		if strings.Contains(got, notWant) {
			t.Errorf("View unexpectedly contains %q:\n%s", notWant, got)
		}
	}
}

// Descriptions live in the info box (i), not the table.
func TestCompanyListModel_View_TableHasNoDescription(t *testing.T) {
	t.Parallel()

	companies := []company.Company{{ID: 1, Name: "Contoso", Description: "Open-core collaboration platform."}}

	got := companyListView(companyListOf(t, companies, nil, 120, 40))
	for _, notWant := range []string{"Description", "Open-core collaboration platform."} {
		if strings.Contains(got, notWant) {
			t.Errorf("View with the info box closed contains %q:\n%s", notWant, got)
		}
	}
}

// Moving between companies with no description, a short one or a long one
// never changes the list's height, with the info box open or closed.
func TestCompanyListModel_View_SameHeightWhateverTheSelectedDescription(t *testing.T) {
	t.Parallel()

	companies := []company.Company{
		{ID: 1, Name: "Blank"},
		{ID: 2, Name: "Short", Description: "Makes widgets."},
		{ID: 3, Name: "Long", Description: strings.Repeat("A company with a very long description that goes on and on. ", 20)},
	}

	for _, infoOpen := range []bool{false, true} {
		m := companyListOf(t, companies, nil, 120, 30)
		if infoOpen {
			press(t, m, runeKey('i'))
		}
		var heights []int
		for range companies {
			heights = append(heights, strings.Count(companyListView(m), "\n"))
			press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
		}
		for i, h := range heights {
			if h != heights[0] {
				t.Errorf("info open=%v: View height with %s selected = %d lines, want %d (same as %s)", infoOpen, companies[i].Name, h, heights[0], companies[0].Name)
			}
		}
	}
}

// Column widths are fixed, so scrolling to a window of shorter names (or
// only never-fetched companies) doesn't make the table change width.
func TestCompanyListModel_View_SameWidthWhateverIsScrolledIntoView(t *testing.T) {
	t.Parallel()

	companies := []company.Company{
		{ID: 1, Name: "Wonka Industries Ltd", LastFetchedAt: time.Date(2026, 9, 24, 1, 9, 0, 0, time.UTC)},
		{ID: 2, Name: "Soylent"},
		{ID: 3, Name: "Stark"},
	}

	// A height with room for one company at a time, so each cursor
	// position scrolls a different name into view: the table's own chrome
	// plus one row, the help line with its margin, and the search line.
	height := postingTableChromeLines + 1 + lipgloss.Height(model.HelpStyle.Render("help")) + 1
	m := companyListOf(t, companies, nil, 0, height)
	var widths []int
	for range companies {
		view := companyListView(m)
		if n := strings.Count(view, "│ ") / 3; n != 2 {
			t.Fatalf("view shows %d table rows (header included), want 2 -- one company at a time:\n%s", n, view)
		}
		firstLine := strings.SplitN(view, "╭", 2)[1]
		firstLine = strings.SplitN(firstLine, "\n", 2)[0]
		widths = append(widths, lipgloss.Width(firstLine))
		press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	for i, w := range widths {
		if w != widths[0] {
			t.Errorf("table width with %s in view = %d, want %d (same as %s)", companies[i].Name, w, widths[0], companies[0].Name)
		}
	}
}

func TestCompanyListModel_I_TogglesInfoBoxWithFullDescription(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("Open-core collaboration and workflow automation. ", 3)
	companies := []company.Company{
		{ID: 1, Name: "Contoso", Source: jobboard.Greenhouse, SourceRef: "contoso", Description: long},
	}
	// The box wraps the description, so compare with whitespace squashed.
	squash := func(s string) string {
		return strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "─", " ").Replace(s)), " ")
	}

	m := companyListOf(t, companies, nil, 120, 40)
	if got := companyListView(m); strings.Contains(got, "greenhouse/contoso") {
		t.Fatalf("info box should start closed:\n%s", got)
	}

	if intent := press(t, m, runeKey('i')); intent != nil {
		t.Fatalf("intent on 'i' = %#v, want nil: the box is this screen's own state", intent)
	}
	got := companyListView(m)
	for _, want := range []string{"Contoso · greenhouse/contoso", strings.TrimSpace(long)} {
		if !strings.Contains(squash(got), want) {
			t.Errorf("open info box missing %q:\n%s", want, got)
		}
	}

	press(t, m, runeKey('i'))
	if got := companyListView(m); strings.Contains(got, "greenhouse/contoso") {
		t.Errorf("second 'i' should close the info box:\n%s", got)
	}
}

// With more companies than fit, opening the info box takes its lines from
// the table rather than making the view taller than the space it's given.
func TestCompanyListModel_View_InfoBoxTakesItsSpaceFromTheTable(t *testing.T) {
	t.Parallel()

	var companies []company.Company
	for i := range 30 {
		companies = append(companies, company.Company{ID: int64(i + 1), Name: fmt.Sprintf("Company %02d", i), Description: "Makes widgets."})
	}

	m := companyListOf(t, companies, nil, 120, 20)
	closed := strings.Count(companyListView(m), "\n")
	press(t, m, runeKey('i'))
	open := strings.Count(companyListView(m), "\n")

	if open != closed {
		t.Errorf("View with info box open = %d lines, want %d (same as closed)", open, closed)
	}
}

// openHighlighted presses enter to keep the matches, then enter again to
// open the highlighted one, returning that intent.
func openHighlighted(t *testing.T, m *companyListModel) tea.Msg {
	t.Helper()
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	return press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// typeKeys sends each rune of s to m as a key press.
func typeKeys(t *testing.T, m *companyListModel, s string) {
	t.Helper()
	for _, r := range s {
		press(t, m, runeKey(r))
	}
}

// Enter keeps the matches and stops typing, as on the all-postings view,
// so letters act on the highlighted match instead of going into the query.
func TestCompanyListModel_Search_EnterKeepsMatchesForCommands(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}, {ID: 3, Name: "Initech"}}, nil, 0, 40)

	typeKeys(t, m, "/glo")
	if intent := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}); intent != nil {
		t.Fatalf("enter while typing: intent = %#v, want none", intent)
	}
	if m.searching || m.query != "glo" {
		t.Fatalf("after enter: searching %v, query %q; want not typing, query \"glo\"", m.searching, m.query)
	}

	intent := press(t, m, runeKey('e'))
	if edit, ok := intent.(message.OpenCompanyEditMsg); !ok || edit.Company.ID != 2 {
		t.Errorf("e on the kept matches: intent = %#v, want message.OpenCompanyEditMsg for Globex", intent)
	}
	intent = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if sel, ok := intent.(message.OpenCompanyPostingsMsg); !ok || sel.Company.ID != 2 {
		t.Errorf("enter on the kept matches: intent = %#v, want message.OpenCompanyPostingsMsg for Globex", intent)
	}
}

func TestCompanyListModel_Search_EscClearsKeptMatchesAndKeepsHighlight(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}, {ID: 3, Name: "Initech"}}, nil, 0, 40)
	typeKeys(t, m, "/glo")
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if intent := press(t, m, tea.KeyPressMsg{Code: tea.KeyEsc}); intent != nil {
		t.Errorf("esc on kept matches: intent = %#v, want none", intent)
	}

	if m.searching || m.query != "" || m.cursor != 1 {
		t.Errorf("after esc: searching %v, query %q, cursor %d; want closed, empty, on Globex (1)", m.searching, m.query, m.cursor)
	}
}

func TestCompanyListModel_View_KeptMatches(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}, {ID: 3, Name: "Initech"}}, nil, 200, 20)
	typeKeys(t, m, "/glo")
	if view := companyListView(m); !strings.Contains(view, "enter: keep results") {
		t.Errorf("typing help doesn't say enter keeps the results:\n%s", view)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	view := ansi.Strip(companyListView(m))
	for _, want := range []string{"🔍 glo  1 of 3", "/: edit search", "esc: clear search", "e: edit"} {
		if !strings.Contains(view, want) {
			t.Errorf("view doesn't show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "▏") || strings.Contains(view, "Initech") {
		t.Errorf("view shows a typing caret or a non-match after enter:\n%s", view)
	}
}

func TestCompanyListModel_Search_ArrowsMoveThroughMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		down, up tea.KeyPressMsg
	}{
		{name: "arrows", down: tea.KeyPressMsg{Code: tea.KeyDown}, up: tea.KeyPressMsg{Code: tea.KeyUp}},
		{name: "ctrl+n/ctrl+p", down: tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, up: tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Acme Robotics"}, {ID: 3, Name: "Globex"}}, nil, 0, 40)

			typeKeys(t, m, "/acme")
			press(t, m, tt.down)
			press(t, m, tt.down) // past the last match: stays on it
			intent := openHighlighted(t, m)
			if sel, ok := intent.(message.OpenCompanyPostingsMsg); !ok || sel.Company.ID != 2 {
				t.Fatalf("intent after down, down, enter = %#v, want message.OpenCompanyPostingsMsg for Acme Robotics (ID 2)", intent)
			}

			press(t, m, runeKey('/'))
			press(t, m, tt.up)
			intent = openHighlighted(t, m)
			if sel, ok := intent.(message.OpenCompanyPostingsMsg); !ok || sel.Company.ID != 1 {
				t.Fatalf("intent after up, enter = %#v, want message.OpenCompanyPostingsMsg for Acme (ID 1)", intent)
			}
		})
	}
}

func TestCompanyListModel_Search_BackspaceWidensMatches(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}, nil, 0, 40)

	typeKeys(t, m, "/gx") // matches nothing
	press(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	intent := openHighlighted(t, m)
	if sel, ok := intent.(message.OpenCompanyPostingsMsg); !ok || sel.Company.ID != 2 {
		t.Fatalf("intent after backspace, enter = %#v, want message.OpenCompanyPostingsMsg for Globex (ID 2)", intent)
	}
}

func TestCompanyListModel_Search_SpaceIsPartOfTheQuery(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Acme Robotics"}}, nil, 0, 40)

	typeKeys(t, m, "/acme")
	press(t, m, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	intent := openHighlighted(t, m)
	if sel, ok := intent.(message.OpenCompanyPostingsMsg); !ok || sel.Company.ID != 2 {
		t.Fatalf("intent after \"acme \", enter = %#v, want message.OpenCompanyPostingsMsg for Acme Robotics (ID 2)", intent)
	}
}

// Esc keeps the highlighted company selected in the full list, so '/'
// also serves to jump to a company and then refresh, edit or delete it.
func TestCompanyListModel_Search_EscClearsAndKeepsHighlight(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}, {ID: 3, Name: "Initech"}}, nil, 0, 40)

	typeKeys(t, m, "/glo")
	if intent := press(t, m, tea.KeyPressMsg{Code: tea.KeyEsc}); intent != nil {
		t.Fatalf("intent on esc in the prompt = %#v, want nil (esc closes the prompt, it doesn't leave the screen)", intent)
	}
	intent := press(t, m, runeKey('e'))
	if edit, ok := intent.(message.OpenCompanyEditMsg); !ok || edit.Company.ID != 2 {
		t.Fatalf("intent on 'e' after esc = %#v, want message.OpenCompanyEditMsg for Globex (ID 2)", intent)
	}
	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	intent = press(t, m, runeKey('e'))
	if edit, ok := intent.(message.OpenCompanyEditMsg); !ok || edit.Company.ID != 3 {
		t.Fatalf("intent on down, 'e' after esc = %#v, want message.OpenCompanyEditMsg for Initech (ID 3): the full list is back", intent)
	}
}

// While the prompt is open, the screen's command letters are just text.
func TestCompanyListModel_Search_CommandLettersAreTyped(t *testing.T) {
	t.Parallel()

	for _, r := range "qdreaiRbjk/" {
		t.Run(string(r), func(t *testing.T) {
			t.Parallel()
			m := companyListOf(t, []company.Company{{ID: 1, Name: "Zzz"}, {ID: 2, Name: "x" + string(r) + "y"}}, nil, 0, 40)

			typeKeys(t, m, "/")
			if intent := press(t, m, runeKey(r)); intent != nil {
				t.Fatalf("intent on %q in the prompt = %#v, want nil", r, intent)
			}
			intent := openHighlighted(t, m)
			if sel, ok := intent.(message.OpenCompanyPostingsMsg); !ok || sel.Company.ID != 2 {
				t.Fatalf("intent on enter after typing %q = %#v, want message.OpenCompanyPostingsMsg for ID 2", r, intent)
			}
		})
	}
}

// A reload arriving while the prompt is open still reaches the list.
func TestCompanyListModel_Search_ReloadWhileTypingIsKept(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}}, nil, 80, 40)
	typeKeys(t, m, "/")

	m.companiesRead(companiesReadOf([]company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}, nil))

	if got := companyListView(m); !strings.Contains(got, "Globex") {
		t.Errorf("View after a reload with the prompt open is missing Globex:\n%s", got)
	}
}

func TestCompanyListModel_View_Search(t *testing.T) {
	t.Parallel()

	companies := []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Acme Robotics"}, {ID: 3, Name: "Globex"}}
	tests := []struct {
		name    string
		keys    string
		want    []string
		notWant []string
	}{
		{name: "prompt open, no query yet", keys: "/", want: []string{"🔍 ", "3 of 3", "Acme", "Globex"}},
		{name: "some match", keys: "/acme", want: []string{"🔍 acme", "2 of 3", "Acme Robotics"}, notWant: []string{"Globex"}},
		{name: "none match", keys: "/zz", want: []string{"🔍 zz", "0 of 3", `No companies match "zz".`}, notWant: []string{"Acme", "Globex", "No companies yet"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := companyListOf(t, companies, nil, 80, 40)
			typeKeys(t, m, tt.keys)
			got := companyListView(m)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("View missing %q:\n%s", want, got)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("View unexpectedly contains %q:\n%s", notWant, got)
				}
			}
		})
	}
}

func TestCompanyListModel_View_HelpMatchesMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keys    string
		want    []string
		notWant []string
	}{
		{name: "list", keys: "", want: []string{"/: search"}},
		{name: "prompt open", keys: "/", want: []string{"type to filter", "enter: keep results", "esc: clear"}, notWant: []string{"d: delete", "q: quit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}}, nil, 0, 40)
			typeKeys(t, m, tt.keys)
			got := companyListView(m)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("View missing %q:\n%s", want, got)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("View unexpectedly contains %q:\n%s", notWant, got)
				}
			}
		})
	}
}

func TestCompanyListModel_View_Search_InfoBoxDescribesHighlightedMatch(t *testing.T) {
	t.Parallel()

	companies := []company.Company{
		{ID: 1, Name: "Acme", Description: "Makes anvils."},
		{ID: 2, Name: "Globex", Description: "Makes doomsday devices."},
	}
	m := companyListOf(t, companies, nil, 80, 40)
	typeKeys(t, m, "i/glo")

	got := companyListView(m)
	if !strings.Contains(got, "Makes doomsday devices.") || strings.Contains(got, "Makes anvils.") {
		t.Errorf("View's info box should describe Globex, the highlighted match:\n%s", got)
	}
}

func TestCompanyListModel_Search_BackspaceOnEmptyQueryCloses(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}}, nil, 0, 40)

	typeKeys(t, m, "/")
	press(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if intent := press(t, m, runeKey('q')); intent != (tea.QuitMsg{}) {
		t.Fatalf("intent on 'q' after backspace on an empty query = %#v, want tea.QuitMsg (the prompt should be closed)", intent)
	}
}

func TestCompanyListModel_View_TableSpansTerminalWidth(t *testing.T) {
	t.Parallel()

	companies := []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}
	tests := []struct {
		name  string
		width int
		want  int
	}{
		{"narrow terminal", 80, 80},
		{"wide terminal", 160, 160},
		{"before the terminal reports its size", 0, fallbackTableWidth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := companyListOf(t, companies, nil, tt.width, 40)
			if got := tableWidthOf(t, companyListView(m)); got != tt.want {
				t.Errorf("table width = %d, want %d", got, tt.want)
			}
		})
	}
}

// tableTop is the line the table starts on in view.
func tableTop(t *testing.T, view string) int {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "╭") {
			return i
		}
	}
	t.Fatalf("no table in view:\n%s", view)
	return 0
}

// The search line is always there, so opening it doesn't push the table
// down.
func TestCompanyListModel_View_SearchLineDoesNotMoveTheTable(t *testing.T) {
	t.Parallel()

	m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}, nil, 80, 40)
	idle := companyListView(m)
	if !strings.Contains(idle, "🔍") {
		t.Errorf("idle view has no search line:\n%s", idle)
	}
	typeKeys(t, m, "/")
	if got, want := tableTop(t, companyListView(m)), tableTop(t, idle); got != want {
		t.Errorf("table starts on line %d with the search open, want %d (as when idle)", got, want)
	}
}

// The info box lines up with the table above it.
func TestCompanyListModel_View_InfoBoxIsAsWideAsTheTable(t *testing.T) {
	t.Parallel()

	companies := []company.Company{{ID: 1, Name: "Acme", Description: strings.Repeat("Makes anvils for roadrunner enthusiasts. ", 3)}}
	for _, width := range []int{80, 160, 0} {
		m := companyListOf(t, companies, nil, width, 40)
		typeKeys(t, m, "i")
		var tops []int
		for _, line := range strings.Split(companyListView(m), "\n") {
			if strings.Contains(line, "╭") {
				tops = append(tops, lipgloss.Width(line))
			}
		}
		if len(tops) != 2 {
			t.Fatalf("width %d: found %d boxes, want the table and the info box", width, len(tops))
		}
		if tops[1] != tops[0] {
			t.Errorf("width %d: info box is %d wide, want %d (the table's width)", width, tops[1], tops[0])
		}
	}
}

// The list takes keys only while the last focus signal named it.
// What changed while another screen was up, such as an archived posting's
// open count, shows when the list is back.
func TestCompanyListModel_ReloadsOnGainingFocus(t *testing.T) {
	t.Parallel()

	other := newCompanyListModel(nil, nil).Instance()
	tests := []struct {
		name       string
		before     func(m *companyListModel) message.ScreenInstance
		after      func(m *companyListModel) message.ScreenInstance
		wantReload bool
	}{
		{name: "comes into focus", before: func(*companyListModel) message.ScreenInstance { return other }, after: (*companyListModel).Instance, wantReload: true},
		{name: "stays in focus", before: (*companyListModel).Instance, after: (*companyListModel).Instance, wantReload: false},
		{name: "loses focus", before: (*companyListModel).Instance, after: func(*companyListModel) message.ScreenInstance { return other }, wantReload: false},
		{name: "stays out of focus", before: func(*companyListModel) message.ScreenInstance { return other }, after: func(*companyListModel) message.ScreenInstance { return 0 }, wantReload: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			m := newCompanyListModel(s.Companies, s.Postings)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}
			r.Dispatch(focusOn(tt.before(m)))

			cmd := r.Dispatch(focusOn(tt.after(m)))

			reloaded := false
			for _, msg := range runCmd(t, cmd) {
				if read, ok := msg.(message.Event[message.Read[message.CompanyList]]); ok {
					assertEventFrom(t, read.Meta, message.Source{Instance: m.Instance()})
					reloaded = true
				}
			}
			if reloaded != tt.wantReload {
				t.Errorf("reloaded = %v, want %v", reloaded, tt.wantReload)
			}
		})
	}
}

// A sync changes the Open and Last fetched columns.
func TestCompanyListModel_ReloadsAfterASync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		msg        tea.Msg
		wantReload bool
	}{
		{name: "a refresh", msg: synced(message.Source{Instance: 9}, 1, 0, 0), wantReload: true},
		{name: "a refused refresh", msg: message.New(message.Source{Instance: 9}, message.Subject{Type: message.TypeCompany, ID: 1}, message.OpNone, time.Now().UTC(), message.Error{Code: message.ErrSyncInProgress}), wantReload: false},
		{name: "a company synced in a run", msg: synced(message.Source{Instance: 9}, 1, 1, 0), wantReload: true},
		{name: "a company that didn't sync in a run", msg: synced(message.Source{Instance: 9}, 1, 1, message.ErrOther), wantReload: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			m := newCompanyListModel(s.Companies, s.Postings)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			reloaded := false
			for _, msg := range runCmd(t, r.Dispatch(tt.msg)) {
				if read, ok := msg.(message.Event[message.Read[message.CompanyList]]); ok {
					assertEventFrom(t, read.Meta, message.Source{Instance: m.Instance()})
					reloaded = true
				}
			}
			if reloaded != tt.wantReload {
				t.Errorf("reloaded = %v, want %v", reloaded, tt.wantReload)
			}
		})
	}
}

func TestCompanyListModel_TakesKeysOnlyWhileFocused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		focused    func(m *companyListModel) message.ScreenInstance
		wantCursor int
	}{
		{name: "the list in focus", focused: func(m *companyListModel) message.ScreenInstance { return m.Instance() }, wantCursor: 1},
		{name: "another component in focus", focused: func(*companyListModel) message.ScreenInstance { return newCompanyListModel(nil, nil).Instance() }, wantCursor: 0},
		{name: "nothing in focus", focused: func(*companyListModel) message.ScreenInstance { return 0 }, wantCursor: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := companyListOf(t, []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}, nil, 0, 40)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(focusOn(tt.focused(m)))
			r.Dispatch(tea.KeyPressMsg{Code: tea.KeyDown})

			if m.cursor != tt.wantCursor {
				t.Errorf("cursor after down = %d, want %d", m.cursor, tt.wantCursor)
			}
		})
	}
}
