package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

var testListCompany = message.Company{ID: 1, Name: "Acme"}

func testPostings() []message.Posting {
	return []message.Posting{
		{ID: 1, Title: "Engineer", ListingStatus: message.ListingOpen},
		{ID: 2, Title: "Designer", ListingStatus: message.ListingOpen},
	}
}

// postingListOf is c's posting list, drawn in width by 40 rows, once it has
// read postings.
func postingListOf(t *testing.T, c message.Company, postings []message.Posting, hideArchived bool, width int) *postingListModel {
	t.Helper()
	m := newPostingListModel(nil, nil, c, hideArchived, message.ContentSize{Width: width, Rows: 40})
	m.postingsRead(postingsReadOf(m, postings))
	return m
}

func postingsReadOf(m *postingListModel, postings []message.Posting) message.Event[message.Read[message.PostingList]] {
	return message.New(message.Source{Instance: m.Instance()}, message.Subject{Type: message.TypeCompany, ID: m.company.ID}, message.OpNone, time.Now().UTC(),
		message.Read[message.PostingList]{Value: message.PostingList{Postings: postings, Markup: map[int64]message.Markup{}}})
}

// pressOn sends key to m and returns the messages its command produces.
func pressOn(t *testing.T, m *postingListModel, key tea.KeyPressMsg) []tea.Msg {
	t.Helper()
	return runCmd(t, m.keyPressed(key))
}

func postingListView(m *postingListModel) string {
	return m.View().Content
}

func TestPostingListModel_CursorMovement_ClampsToPostings(t *testing.T) {
	t.Parallel()

	m := postingListOf(t, testListCompany, testPostings(), false, 0)

	pressOn(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	pressOn(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor after two downs = %d, want 1 (clamped)", m.cursor)
	}

	pressOn(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	pressOn(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor after two ups = %d, want 0 (clamped)", m.cursor)
	}
}

func TestPostingListModel_Enter_PushesDetailSteppingThroughTheList(t *testing.T) {
	t.Parallel()

	m := postingListOf(t, testListCompany, testPostings(), false, 0)
	pressOn(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	got := pressOn(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(got) != 1 {
		t.Fatalf("enter sent %#v, want one Nav", got)
	}
	nav, ok := got[0].(message.Event[message.Nav[message.PostingDetail]])
	if !ok {
		t.Fatalf("enter sent %T, want a Nav to posting detail", got[0])
	}
	assertEventFrom(t, nav.Meta, message.Source{Instance: m.Instance()})
	designer := testPostings()[1]
	if want := (message.Subject{Type: message.TypePosting, ID: designer.ID}); nav.Meta.Subject != want {
		t.Errorf("Subject = %+v, want %+v", nav.Meta.Subject, want)
	}
	want := message.Nav[message.PostingDetail]{Op: message.NavPush, Screen: message.ScreenPostingDetail,
		Value: message.PostingDetail{Posting: designer, Siblings: []int64{1, 2}}}
	if diff := cmp.Diff(want, nav.Payload); diff != "" {
		t.Errorf("Nav (-want +got):\n%s", diff)
	}
}

// Going back from a detail that stepped lands on the posting it showed
// last.
func TestPostingListModel_DetailOpened_MovesTheCursorToItsPosting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		postingID int64
		want      int
	}{
		{name: "a posting it lists", postingID: 2, want: 1},
		{name: "one it doesn't", postingID: 99, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := postingListOf(t, testListCompany, testPostings(), false, 0)

			m.detailOpened(message.New(message.Source{Instance: 9}, message.Subject{Type: message.TypePosting, ID: tt.postingID}, message.OpNone, time.Now().UTC(),
				message.Nav[message.PostingDetail]{Op: message.NavReplace, Screen: message.ScreenPostingDetail, Value: message.PostingDetail{Posting: message.Posting{ID: tt.postingID}}}))
			if m.cursor != tt.want {
				t.Errorf("cursor = %d, want %d", m.cursor, tt.want)
			}
		})
	}
}

func TestPostingListModel_F_PushesThePickerOnTheListsFilters(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil, nil, testListCompany, false, message.ContentSize{Rows: 40})
	read := postingsReadOf(m, testPostings())
	read.Payload.Value.Departments, read.Payload.Value.Locations = []string{"Engineering"}, []string{"Remote"}
	m.postingsRead(read)

	got := pressOn(t, m, runeKey('f'))
	if len(got) != 1 {
		t.Fatalf("f sent %#v, want one Nav", got)
	}
	nav, ok := got[0].(message.Event[message.Nav[message.FilterSelection]])
	if !ok {
		t.Fatalf("f sent %T, want a Nav to the filter picker", got[0])
	}
	assertEventFrom(t, nav.Meta, message.Source{Instance: m.Instance()})
	if want := (message.Subject{Type: message.TypeCompany, ID: testListCompany.ID}); nav.Meta.Subject != want {
		t.Errorf("Subject = %+v, want %+v", nav.Meta.Subject, want)
	}
	want := message.Nav[message.FilterSelection]{Op: message.NavPush, Screen: message.ScreenFilterSelect, Value: message.FilterSelection{
		Company: testListCompany, Departments: []string{"Engineering"}, Locations: []string{"Remote"},
	}}
	if diff := cmp.Diff(want, nav.Payload); diff != "" {
		t.Errorf("Nav (-want +got):\n%s", diff)
	}
}

func TestPostingListModel_EscOrB_Pops(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('b')} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			m := postingListOf(t, testListCompany, testPostings(), false, 0)

			if got := pressOn(t, m, key); !popsIn(got, m.Instance()) {
				t.Errorf("%s sent %#v, want a pop of the list", key, got)
			}
		})
	}
}

func TestPostingListModel_R_AsksToSyncItsCompany(t *testing.T) {
	t.Parallel()

	m := postingListOf(t, testListCompany, testPostings(), false, 0)

	got := pressOn(t, m, runeKey('r'))
	if len(got) != 1 {
		t.Fatalf("r sent %#v, want one request", got)
	}
	run, ok := got[0].(message.Event[message.Run[message.SyncCompany]])
	if !ok {
		t.Fatalf("r sent %T, want message.Event[message.Run[message.SyncCompany]]", got[0])
	}
	assertEventFrom(t, run.Meta, message.Source{Instance: m.Instance()})
	if want := (message.Subject{Type: message.TypeCompany, ID: testListCompany.ID}); run.Meta.Subject != want || run.Payload.Value.Company != testListCompany {
		t.Errorf("r sent %+v, want a sync of %+v about %+v", run, testListCompany, want)
	}
}

// listWith is a posting list registered on a router, for a company with
// two postings in the store.
func listWith(t *testing.T, hideArchived bool) (*postingListModel, *model.MsgRouter) {
	t.Helper()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	m := newPostingListModel(s.Companies, s.Postings, companySnapshot(acme), hideArchived, message.ContentSize{Rows: 40})
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return m, r
}

// readsFrom is whether msgs hold m's read of its postings.
func readsFrom(t *testing.T, m *postingListModel, msgs []tea.Msg) bool {
	t.Helper()
	read := false
	for _, msg := range msgs {
		if e, ok := msg.(message.Event[message.Read[message.PostingList]]); ok {
			assertEventFrom(t, e.Meta, message.Source{Instance: m.Instance()})
			read = true
		}
	}
	return read
}

func TestPostingListModel_Init_ReadsItsCompanysPostings(t *testing.T) {
	t.Parallel()

	m, r := listWith(t, false)
	for _, msg := range runCmd(t, m.Init()) {
		r.Dispatch(msg)
	}

	var titles []string
	for _, p := range m.list.Postings {
		titles = append(titles, p.Title)
	}
	if len(titles) != 2 {
		t.Errorf("postings after Init = %v, want Engineer and Designer", titles)
	}
}

// A sync of the list's company can have written postings it shows; one of
// another company can't.
func TestPostingListModel_ReloadsAfterASyncOfItsCompany(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		msg        func(m *postingListModel) tea.Msg
		wantReload bool
	}{
		{name: "its company, refreshed", msg: func(m *postingListModel) tea.Msg { return synced(message.Source{Instance: 9}, m.company.ID, 0, 0) }, wantReload: true},
		{name: "its company, synced in a run", msg: func(m *postingListModel) tea.Msg { return synced(message.Source{Instance: 9}, m.company.ID, 1, 0) }, wantReload: true},
		{name: "another company, refreshed", msg: func(m *postingListModel) tea.Msg { return synced(message.Source{Instance: 9}, m.company.ID+1, 0, 0) }, wantReload: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, r := listWith(t, false)

			if got := readsFrom(t, m, runCmd(t, r.Dispatch(tt.msg(m)))); got != tt.wantReload {
				t.Errorf("reloaded = %v, want %v", got, tt.wantReload)
			}
		})
	}
}

func TestPostingListModel_A_FlipsHideArchivedAndTellsApp(t *testing.T) {
	t.Parallel()

	m, _ := listWith(t, true)

	got := pressOn(t, m, runeKey('A'))
	if m.hideArchived {
		t.Error("hideArchived after A = true, want false")
	}
	if !readsFrom(t, m, got) {
		t.Error("A sent no read of the postings")
	}
	var told []message.HideArchived
	for _, msg := range got {
		if e, ok := msg.(message.Event[message.Signal[message.HideArchived]]); ok {
			told = append(told, e.Payload.Value)
		}
	}
	if diff := cmp.Diff([]message.HideArchived{{Hidden: false}}, told); diff != "" {
		t.Errorf("A signalled (-want +got):\n%s", diff)
	}
}

// A read of the postings again leaves the cursor on the posting it was
// on, though one now sorts ahead of it.
func TestPostingListModel_Read_KeepsTheCursorOnItsPosting(t *testing.T) {
	t.Parallel()

	m := postingListOf(t, testListCompany, testPostings(), false, 0)
	pressOn(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	m.postingsRead(postingsReadOf(m, append([]message.Posting{{ID: 3, Title: "Analyst"}}, testPostings()...)))
	if got := m.selected(); got != 2 {
		t.Errorf("posting under the cursor = %d, want 2, the one it was on", got)
	}
}

// The list hears a markup change whether or not it's on top.
func TestPostingListModel_MarkupChanged_PatchesItsRow(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	tests := []struct {
		name         string
		hideArchived bool
		after        message.Markup
		wantTitles   []string
		wantMarker   string
	}{
		{name: "interested", after: message.Markup{PostingID: 2, InterestedAt: now}, wantTitles: []string{"Engineer", "Designer"}, wantMarker: "★"},
		{name: "archived, archived shown", after: message.Markup{PostingID: 2, ArchivedAt: now}, wantTitles: []string{"Engineer", "Designer"}, wantMarker: "✕"},
		{name: "archived, archived hidden", hideArchived: true, after: message.Markup{PostingID: 2, ArchivedAt: now}, wantTitles: []string{"Engineer"}},
		{name: "a posting it doesn't list", after: message.Markup{PostingID: 9, InterestedAt: now}, wantTitles: []string{"Engineer", "Designer"}, wantMarker: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := postingListOf(t, testListCompany, testPostings(), tt.hideArchived, 0)
			pressOn(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(message.New(message.Source{}, message.Subject{Type: message.TypePosting, ID: tt.after.PostingID}, message.OpUpdated, now,
				message.Changed[message.Markup]{Before: message.Markup{PostingID: tt.after.PostingID}, After: tt.after}))

			var titles []string
			for _, p := range m.list.Postings {
				titles = append(titles, p.Title)
			}
			if diff := cmp.Diff(tt.wantTitles, titles); diff != "" {
				t.Errorf("postings (-want +got):\n%s", diff)
			}
			if m.cursor >= len(m.list.Postings) {
				t.Errorf("cursor = %d, past the %d postings", m.cursor, len(m.list.Postings))
			}
			if tt.wantMarker != "" {
				if got := postingMarker(m.list.Markup[2]); got != tt.wantMarker {
					t.Errorf("Designer's marker = %q, want %q", got, tt.wantMarker)
				}
			}
		})
	}
}

func TestPostingListModel_IAndX_MarkThePostingUnderTheCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key        rune
		wantMarker string
	}{
		{key: 'i', wantMarker: "★"},
		{key: 'x', wantMarker: "✕"},
	}
	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			t.Parallel()
			m, r := listWith(t, false)
			for _, msg := range runCmd(t, m.Init()) {
				r.Dispatch(msg)
			}
			marked := m.list.Postings[0]

			got := pressOn(t, m, runeKey(tt.key))
			if len(got) != 1 {
				t.Fatalf("%c sent %#v, want one change", tt.key, got)
			}
			e, ok := got[0].(message.Event[message.Changed[message.Markup]])
			if !ok {
				t.Fatalf("%c sent %T, want message.Event[message.Changed[message.Markup]]", tt.key, got[0])
			}
			assertEventFrom(t, e.Meta, message.Source{Instance: m.Instance()})
			if want := (message.Subject{Type: message.TypePosting, ID: marked.ID}); e.Meta.Subject != want || e.Meta.Op != message.OpUpdated {
				t.Errorf("Subject, Op = %+v, %v, want %+v, %v", e.Meta.Subject, e.Meta.Op, want, message.OpUpdated)
			}
			if diff := cmp.Diff(message.Markup{PostingID: marked.ID}, e.Payload.Before); diff != "" {
				t.Errorf("Before (-want +got):\n%s", diff)
			}
			r.Dispatch(e)
			if got := postingMarker(m.list.Markup[marked.ID]); got != tt.wantMarker {
				t.Errorf("%s's marker = %q, want %q", marked.Title, got, tt.wantMarker)
			}
		})
	}
}

func TestMarkupToggles_OnAMissingPosting_SendAnError(t *testing.T) {
	t.Parallel()

	src := message.Source{Instance: 7}
	const missing = 999
	tests := []struct {
		name   string
		toggle func(*service.PostingService) tea.Cmd
	}{
		{name: "interested", toggle: func(p *service.PostingService) tea.Cmd { return toggleInterested(p, src, missing, false) }},
		{name: "archived", toggle: func(p *service.PostingService) tea.Cmd { return toggleArchived(p, src, missing, false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)

			got := runCmd(t, tt.toggle(s.Postings))
			if len(got) != 1 {
				t.Fatalf("sent %#v, want one error", got)
			}
			e, ok := got[0].(message.Event[message.Error])
			if !ok {
				t.Fatalf("sent %T, want message.Event[message.Error]", got[0])
			}
			assertEventFrom(t, e.Meta, src)
			if want := (message.Subject{Type: message.TypePosting, ID: missing}); e.Meta.Subject != want || e.Payload.Code != message.ErrNotFound {
				t.Errorf("Subject, Code = %+v, %v, want %+v, %v", e.Meta.Subject, e.Payload.Code, want, message.ErrNotFound)
			}
		})
	}
}

func TestPostingListModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, newPostingListModel(nil, nil, testListCompany, false, message.ContentSize{}))
}

func TestPostingListModel_ClampCursor(t *testing.T) {
	t.Parallel()

	m := &postingListModel{cursor: 1}
	m.clampCursor(1)
	if m.cursor != 0 {
		t.Fatalf("cursor after clamp = %d, want 0", m.cursor)
	}
}

func TestTruncateCol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"shorter than max is unchanged", "Engineer", 10, "Engineer"},
		{"equal to max is unchanged", "Engineer", 8, "Engineer"},
		{"longer than max is truncated with ellipsis", "Senior Backend Engineer", 10, "Senior Ba…"},
		{"empty string is unchanged", "", 10, ""},
		{"wide (double-width) runes truncate by display width, not rune count", "シニアコンサルティングエンジニア", 10, "シニアコ…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateCol(tt.s, tt.max)
			if got != tt.want {
				t.Fatalf("truncateCol(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
			if w := lipgloss.Width(got); w > tt.max {
				t.Fatalf("truncateCol(%q, %d) width = %d, want <= %d", tt.s, tt.max, w, tt.max)
			}
		})
	}
}

func describedCompany(description string) message.Company {
	c := testListCompany
	c.Description = description
	return c
}

func TestPostingListModel_View_ShowsCompanyDescription(t *testing.T) {
	t.Parallel()

	description := "Acme builds rockets for roadrunner enthusiasts."
	m := postingListOf(t, describedCompany(description), testPostings(), false, 0)

	if got := postingListView(m); !strings.Contains(got, description) {
		t.Errorf("View missing company description %q:\n%s", description, got)
	}
}

// The description line costs the table exactly one row, so it must render
// as exactly one line.
func TestPostingListModel_View_DescriptionAddsExactlyOneLine(t *testing.T) {
	t.Parallel()

	without := strings.Count(postingListView(postingListOf(t, testListCompany, testPostings(), false, 0)), "\n")
	with := strings.Count(postingListView(postingListOf(t, describedCompany("Acme builds rockets for roadrunner enthusiasts."), testPostings(), false, 0)), "\n")

	if with != without+1 {
		t.Errorf("View with description = %d lines, want %d (one more than without)", with, without+1)
	}
}

// The table spans the terminal, so every list is as wide as the others
// and long titles get the room there is.
func TestPostingListModel_View_TableSpansTerminalWidth(t *testing.T) {
	t.Parallel()

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

			m := postingListOf(t, testListCompany, testPostings(), false, tt.width)
			if got := tableWidthOf(t, postingListView(m)); got != tt.want {
				t.Errorf("table width = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPostingListModel_View_LongTitleFitsWhenThereIsRoom(t *testing.T) {
	t.Parallel()

	title := "Senior Software Engineer, Payments Infrastructure and Reliability (Remote)"
	tests := []struct {
		name     string
		width    int
		wantFull bool
	}{
		{"wide terminal shows it in full", 160, true},
		{"narrow terminal cuts it off", 80, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			postings := testPostings()
			postings[0].Title = title
			view := postingListView(postingListOf(t, testListCompany, postings, false, tt.width))
			if got := strings.Contains(view, title); got != tt.wantFull {
				t.Errorf("full title in view = %v, want %v:\n%s", got, tt.wantFull, view)
			}
			if !tt.wantFull && !strings.Contains(view, "…") {
				t.Errorf("cut-off title has no ellipsis:\n%s", view)
			}
		})
	}
}

// The description line is cut off at the table's width, not short of it.
func TestPostingListModel_View_DescriptionUsesTableWidth(t *testing.T) {
	t.Parallel()

	description := strings.TrimSpace(strings.Repeat("Acme builds rockets. ", 7))
	view := postingListView(postingListOf(t, describedCompany(description), testPostings(), false, 160))
	if !strings.Contains(view, description) {
		t.Errorf("View at width 160 missing the %d-column description in full:\n%s", len(description), view)
	}
}

func TestPostingListModel_View_AdvertisesRefresh(t *testing.T) {
	t.Parallel()

	if got := postingListView(postingListOf(t, testListCompany, testPostings(), false, 100)); !strings.Contains(got, "r: refresh") {
		t.Errorf("View() doesn't advertise r: refresh:\n%s", got)
	}
	if got := postingListView(postingListOf(t, testListCompany, nil, false, 100)); !strings.Contains(got, "No postings yet. Press 'r' to refresh.") {
		t.Errorf("empty View() = %q, want it to point at r on this screen", got)
	}
}

// A posting written elsewhere is read again if it's one of the list's
// company's; a sync of another company's postings leaves it alone.
func TestPostingListModel_ReadsAgainForItsCompanysPostingsWrittenElsewhere(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		of   func(m *postingListModel) int64
		read bool
	}{
		{name: "its company's", of: func(m *postingListModel) int64 { return m.company.ID }, read: true},
		{name: "another company's", of: func(m *postingListModel) int64 { return m.company.ID + 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, r := listWith(t, false)
			changed := message.Logged(1, mcp, message.Subject{Type: message.TypePosting, ID: 99}, message.OpCreated, time.Now().UTC(), message.Posting{ID: 99, CompanyID: tt.of(m)})

			if got := readsFrom(t, m, runCmd(t, r.Dispatch(changed))); got != tt.read {
				t.Errorf("read = %t, want %t", got, tt.read)
			}
		})
	}
}

func TestPostingListModel_ChangesElsewhere_ReadOnceAtATime(t *testing.T) {
	t.Parallel()
	m, r := listWith(t, false)
	assertReadsOnceAtATime[message.PostingList](t, r,
		message.Logged(1, mcp, message.Subject{Type: message.TypePosting, ID: 99}, message.OpCreated, time.Now().UTC(), message.Posting{ID: 99, CompanyID: m.company.ID}))
}
