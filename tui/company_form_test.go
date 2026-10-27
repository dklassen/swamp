package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

func TestCompanyFormModel_New_FocusesSourceFieldFirst(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	if m.focus != formFieldSource {
		t.Fatalf("focus = %d, want formFieldSource", m.focus)
	}
	if companySources[m.sourceIndex] != jobboard.Ashby {
		t.Fatalf("default source = %q, want %q", companySources[m.sourceIndex], jobboard.Ashby)
	}
}

func TestCompanyFormModel_Tab_CyclesThroughAllThreeFields(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)

	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != formFieldName {
		t.Fatalf("focus after 1 tab = %d, want formFieldName", m.focus)
	}
	if !m.inputs[formFieldName].Focused() {
		t.Fatal("name field should be focused")
	}

	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != formFieldSourceRef {
		t.Fatalf("focus after 2 tabs = %d, want formFieldSourceRef", m.focus)
	}
	if m.inputs[formFieldName].Focused() {
		t.Fatal("name field should be blurred after tabbing away")
	}
	if !m.inputs[formFieldSourceRef].Focused() {
		t.Fatal("sourceRef field should be focused")
	}

	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != formFieldSource {
		t.Fatalf("focus after 3 tabs = %d, want formFieldSource (wrapped around)", m.focus)
	}
	if m.inputs[formFieldSourceRef].Focused() {
		t.Fatal("sourceRef field should be blurred after tabbing away")
	}
}

func TestCompanyFormModel_RightArrow_OnSourceField_CyclesSource(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	if companySources[m.sourceIndex] != jobboard.Ashby {
		t.Fatalf("starting source = %q, want ashby", companySources[m.sourceIndex])
	}

	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyRight})
	if companySources[m.sourceIndex] != jobboard.Greenhouse {
		t.Fatalf("source after right = %q, want greenhouse", companySources[m.sourceIndex])
	}

	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyRight})
	if companySources[m.sourceIndex] != jobboard.Lever {
		t.Fatalf("source after second right = %q, want lever", companySources[m.sourceIndex])
	}

	// Wraps back around rather than clamping.
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyRight})
	if companySources[m.sourceIndex] != jobboard.Ashby {
		t.Fatalf("source after third right = %q, want ashby (wrapped)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_LeftArrow_OnSourceField_CyclesSourceBackward(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyLeft})
	if companySources[m.sourceIndex] != jobboard.Lever {
		t.Fatalf("source after left = %q, want lever (wrapped backward to the last source)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_LeftRightArrow_OffSourceField_DoesNotChangeSource(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyTab}) // move focus to name field
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyRight})

	if companySources[m.sourceIndex] != jobboard.Ashby {
		t.Fatalf("source = %q, want ashby unchanged (arrow keys should edit the name field's cursor position, not the source)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_Enter_EmptyFields_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("cmd = %v, want nil (empty fields)", cmd)
	}
}

func TestCompanyFormModel_Enter_ValidFields_ReturnsCreateCompanyCmd(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme")

	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}
}

func TestCompanyFormModel_Enter_CreatesCompanyWithSelectedSource(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	m := newCompanyFormModel(newFormTestSyncer(s))
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyRight}) // switch source to greenhouse
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme-token")

	cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}

	created := companyCreatedBy(t, m, cmd())
	if created.Source != jobboard.Greenhouse {
		t.Fatalf("created company Source = %q, want %q", created.Source, jobboard.Greenhouse)
	}

	stored, err := s.Companies.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Source != jobboard.Greenhouse || stored.SourceRef != "acme-token" {
		t.Fatalf("stored company = %+v, want Source=greenhouse SourceRef=acme-token", stored)
	}
}

func TestCompanyFormModel_Enter_CreatesCompanyWithLeverSource(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	m := newCompanyFormModel(newFormTestSyncer(s))
	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyLeft}) // wraps backward to lever, the last source
	if companySources[m.sourceIndex] != jobboard.Lever {
		t.Fatalf("source after left = %q, want lever", companySources[m.sourceIndex])
	}
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}

	created := companyCreatedBy(t, m, cmd())
	if created.Source != jobboard.Lever {
		t.Fatalf("created company Source = %q, want %q", created.Source, jobboard.Lever)
	}

	stored, err := s.Companies.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Source != jobboard.Lever || stored.SourceRef != "acme" {
		t.Fatalf("stored company = %+v, want Source=lever SourceRef=acme", stored)
	}
}

func TestCompanyFormModel_Enter_WhitespaceOnlyName_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.inputs[formFieldName].SetValue("   ")
	m.inputs[formFieldSourceRef].SetValue("acme")

	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("cmd = %v, want nil (whitespace-only name)", cmd)
	}
}

func TestCompanyFormModel_Enter_TrimsSavedName(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	m := newCompanyFormModel(newFormTestSyncer(s))
	m.inputs[formFieldName].SetValue("  Acme  ")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}
	if created := companyCreatedBy(t, m, cmd()); created.Name != "Acme" {
		t.Fatalf("created company Name = %q, want %q (trimmed)", created.Name, "Acme")
	}
}

// companyCreatedBy is the company msg says m created, failing t unless
// msg is m's create with an ID, a time in UTC and the company's CreatedAt.
func companyCreatedBy(t *testing.T, m *companyFormModel, msg tea.Msg) message.Company {
	t.Helper()
	created, ok := msg.(message.Event[message.Changed[message.Company]])
	if !ok {
		t.Fatalf("msg = %#v, want message.Event[message.Changed[message.Company]]", msg)
	}
	assertEventFrom(t, created.Meta, message.Source{Instance: m.Instance()})
	after := created.Payload.After
	if created.Meta.Op != message.OpCreated || created.Meta.Subject != (message.Subject{Type: message.TypeCompany, ID: after.ID}) {
		t.Errorf("Meta = %+v, want %v of company %d", created.Meta, message.OpCreated, after.ID)
	}
	if !created.Meta.At.Equal(after.CreatedAt) {
		t.Errorf("Meta.At = %v, want After.CreatedAt %v", created.Meta.At, after.CreatedAt)
	}
	return after
}

func TestCompanyFormModel_Enter_BoardRejectsSourceRef_NoCompanyCreated(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	syncer := sync.New(s.Companies, s.Postings, map[string]sync.PostingFetcher{
		jobboard.Ashby: &fakeFetcher{errBoards: map[string]error{"acmee": errors.New("404 not found")}},
	}, sync.DefaultConfig())
	m := newCompanyFormModel(syncer)
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acmee")

	cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that checks the board and creates the company")
	}
	failed, ok := cmd().(message.Event[message.Error])
	if !ok {
		t.Fatal("msg is not a message.Event[message.Error], want one for a board that doesn't resolve")
	}
	assertEventFrom(t, failed.Meta, message.Source{Instance: m.Instance()})
	if failed.Meta.Op != message.OpCreated {
		t.Errorf("Meta.Op = %v, want %v", failed.Meta.Op, message.OpCreated)
	}

	companies, err := s.Companies.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 0 {
		t.Fatalf("companies = %+v, want none created", companies)
	}
}

// newFormTestSyncer returns a syncer whose board check accepts any source
// ref for every source the form can pick.
func newFormTestSyncer(s testutil.TestApp) *sync.Syncer {
	fetchers := make(map[string]sync.PostingFetcher, len(companySources))
	for _, src := range companySources {
		fetchers[src] = &fakeFetcher{}
	}
	return sync.New(s.Companies, s.Postings, fetchers, sync.DefaultConfig())
}

func TestCompanyFormModel_WhileBoardCheckInFlight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  rune
	}{
		{name: "enter does not dispatch a second check", key: tea.KeyEnter},
		{name: "esc does not cancel", key: tea.KeyEsc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyFormModel(nil)
			m.inputs[formFieldName].SetValue("Acme")
			m.inputs[formFieldSourceRef].SetValue("acme")
			if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
				t.Fatal("first enter: cmd = nil, want the board check")
			}

			if cmd := m.keyPressed(tea.KeyPressMsg{Code: tt.key}); cmd != nil {
				t.Fatalf("cmd = %v, want nil while the check is in flight", cmd)
			}
		})
	}
}

// The form takes keys only while the last focus signal named it.
func TestCompanyFormModel_TakesKeysOnlyWhileFocused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		focused   func(m *companyFormModel) message.ScreenInstance
		wantClose bool
	}{
		{name: "the form in focus", focused: func(m *companyFormModel) message.ScreenInstance { return m.Instance() }, wantClose: true},
		{name: "another component in focus", focused: func(*companyFormModel) message.ScreenInstance { return newCompanyListModel(nil, nil).Instance() }, wantClose: false},
		{name: "nothing in focus", focused: func(*companyFormModel) message.ScreenInstance { return 0 }, wantClose: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyFormModel(nil)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(focusOn(tt.focused(m)))
			cmd := r.Dispatch(tea.KeyPressMsg{Code: tea.KeyEsc})

			if closed := slices.Contains(runCmd(t, cmd), tea.Msg(message.CloseCompanyFormMsg{})); closed != tt.wantClose {
				t.Errorf("esc asked to close = %v, want %v", closed, tt.wantClose)
			}
		})
	}
}

// A result from an earlier opening of the form leaves this one as it is:
// still checking, and open.
func TestCompanyFormModel_ActsOnItsOwnResultOnly(t *testing.T) {
	t.Parallel()

	created := func(instance message.ScreenInstance) tea.Msg {
		return message.Created(message.Source{Instance: instance}, 1, time.Now().UTC(), message.Company{ID: 1, Name: "Acme"})
	}
	tests := []struct {
		name         string
		result       func(m *companyFormModel) tea.Msg
		wantClose    bool
		wantChecking bool
	}{
		{name: "its own, saved", result: func(m *companyFormModel) tea.Msg { return created(m.Instance()) }, wantClose: true},
		{name: "its own, failed", result: func(m *companyFormModel) tea.Msg {
			return message.New(message.Source{Instance: m.Instance()}, message.Subject{}, message.OpCreated, time.Now().UTC(),
				model.ErrorOf(errors.New("404 not found")))
		}},
		{name: "an earlier form's", result: func(*companyFormModel) tea.Msg {
			return created(newCompanyFormModel(nil).Instance())
		}, wantChecking: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyFormModel(nil)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}
			m.inputs[formFieldName].SetValue("Acme")
			m.inputs[formFieldSourceRef].SetValue("acme")
			if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
				t.Fatal("enter: cmd = nil, want the board check")
			}

			closed := slices.Contains(runCmd(t, r.Dispatch(tt.result(m))), tea.Msg(message.CloseCompanyFormMsg{}))

			if closed != tt.wantClose {
				t.Errorf("asked to close = %v, want %v", closed, tt.wantClose)
			}
			if checking := strings.Contains(m.View().Content, "Checking board"); checking != tt.wantChecking {
				t.Errorf("view shows checking = %v, want %v", checking, tt.wantChecking)
			}
		})
	}
}
