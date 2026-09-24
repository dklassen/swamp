package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

func TestCompanyFormModel_New_FocusesSourceFieldFirst(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	if m.focus != formFieldSource {
		t.Fatalf("focus = %d, want formFieldSource", m.focus)
	}
	if companySources[m.sourceIndex] != "ashby" {
		t.Fatalf("default source = %q, want %q", companySources[m.sourceIndex], "ashby")
	}
}

func TestCompanyFormModel_Tab_CyclesThroughAllThreeFields(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != formFieldName {
		t.Fatalf("focus after 1 tab = %d, want formFieldName", m.focus)
	}
	if !m.inputs[formFieldName].Focused() {
		t.Fatal("name field should be focused")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != formFieldSourceRef {
		t.Fatalf("focus after 2 tabs = %d, want formFieldSourceRef", m.focus)
	}
	if m.inputs[formFieldName].Focused() {
		t.Fatal("name field should be blurred after tabbing away")
	}
	if !m.inputs[formFieldSourceRef].Focused() {
		t.Fatal("sourceRef field should be focused")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
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
	if companySources[m.sourceIndex] != "ashby" {
		t.Fatalf("starting source = %q, want ashby", companySources[m.sourceIndex])
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if companySources[m.sourceIndex] != "greenhouse" {
		t.Fatalf("source after right = %q, want greenhouse", companySources[m.sourceIndex])
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if companySources[m.sourceIndex] != "lever" {
		t.Fatalf("source after second right = %q, want lever", companySources[m.sourceIndex])
	}

	// Wraps back around rather than clamping.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if companySources[m.sourceIndex] != "ashby" {
		t.Fatalf("source after third right = %q, want ashby (wrapped)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_LeftArrow_OnSourceField_CyclesSourceBackward(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if companySources[m.sourceIndex] != "lever" {
		t.Fatalf("source after left = %q, want lever (wrapped backward to the last source)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_LeftRightArrow_OffSourceField_DoesNotChangeSource(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // move focus to name field
	m.Update(tea.KeyMsg{Type: tea.KeyRight})

	if companySources[m.sourceIndex] != "ashby" {
		t.Fatalf("source = %q, want ashby unchanged (arrow keys should edit the name field's cursor position, not the source)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelCompanyFormMsg); !ok {
		t.Fatalf("intent = %T, want cancelCompanyFormMsg", intent)
	}
}

func TestCompanyFormModel_Enter_EmptyFields_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (empty fields)", cmd, intent)
	}
}

func TestCompanyFormModel_Enter_ValidFields_ReturnsCreateCompanyCmd(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestCompanyFormModel_Enter_CreatesCompanyWithSelectedSource(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	m := newCompanyFormModel(newFormTestSyncer(s))
	m.Update(tea.KeyMsg{Type: tea.KeyRight}) // switch source to greenhouse
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme-token")

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}

	msg := cmd()
	created, ok := msg.(companyCreatedMsg)
	if !ok {
		t.Fatalf("msg = %T, want companyCreatedMsg", msg)
	}
	if created.err != nil {
		t.Fatalf("companyCreatedMsg.err = %v, want nil", created.err)
	}
	if created.company.Source != "greenhouse" {
		t.Fatalf("created company Source = %q, want %q", created.company.Source, "greenhouse")
	}

	stored, err := s.GetCompany(context.Background(), created.company.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Source != "greenhouse" || stored.SourceRef != "acme-token" {
		t.Fatalf("stored company = %+v, want Source=greenhouse SourceRef=acme-token", stored)
	}
}

func TestCompanyFormModel_Enter_CreatesCompanyWithLeverSource(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	m := newCompanyFormModel(newFormTestSyncer(s))
	m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // wraps backward to lever, the last source
	if companySources[m.sourceIndex] != "lever" {
		t.Fatalf("source after left = %q, want lever", companySources[m.sourceIndex])
	}
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}

	msg := cmd()
	created, ok := msg.(companyCreatedMsg)
	if !ok {
		t.Fatalf("msg = %T, want companyCreatedMsg", msg)
	}
	if created.err != nil {
		t.Fatalf("companyCreatedMsg.err = %v, want nil", created.err)
	}
	if created.company.Source != "lever" {
		t.Fatalf("created company Source = %q, want %q", created.company.Source, "lever")
	}

	stored, err := s.GetCompany(context.Background(), created.company.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Source != "lever" || stored.SourceRef != "acme" {
		t.Fatalf("stored company = %+v, want Source=lever SourceRef=acme", stored)
	}
}

func TestCompanyFormModel_Enter_WhitespaceOnlyName_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.inputs[formFieldName].SetValue("   ")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (whitespace-only name)", cmd, intent)
	}
}

func TestCompanyFormModel_Enter_TrimsSavedName(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	m := newCompanyFormModel(newFormTestSyncer(s))
	m.inputs[formFieldName].SetValue("  Acme  ")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}
	msg := cmd()
	created, ok := msg.(companyCreatedMsg)
	if !ok {
		t.Fatalf("msg = %T, want companyCreatedMsg", msg)
	}
	if created.err != nil {
		t.Fatalf("companyCreatedMsg.err = %v, want nil", created.err)
	}
	if created.company.Name != "Acme" {
		t.Fatalf("created company Name = %q, want %q (trimmed)", created.company.Name, "Acme")
	}
}

func TestCompanyFormModel_Enter_BoardRejectsSourceRef_NoCompanyCreated(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	syncer := sync.New(s, map[string]sync.PostingFetcher{
		"ashby": &fakeFetcher{errBoards: map[string]error{"acmee": errors.New("404 not found")}},
	})
	m := newCompanyFormModel(syncer)
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acmee")

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that checks the board and creates the company")
	}
	created, ok := cmd().(companyCreatedMsg)
	if !ok {
		t.Fatal("msg is not a companyCreatedMsg")
	}
	if created.err == nil {
		t.Fatal("companyCreatedMsg.err = nil, want an error for a board that doesn't resolve")
	}

	companies, err := s.ListActiveCompanies(context.Background())
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 0 {
		t.Fatalf("companies = %+v, want none created", companies)
	}
}

// newFormTestSyncer returns a syncer whose board check accepts any source
// ref for every source the form can pick.
func newFormTestSyncer(s *store.Store) *sync.Syncer {
	fetchers := make(map[string]sync.PostingFetcher, len(companySources))
	for _, src := range companySources {
		fetchers[src] = &fakeFetcher{}
	}
	return sync.New(s, fetchers)
}

func TestCompanyFormModel_WhileBoardCheckInFlight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  tea.KeyType
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
			if cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
				t.Fatal("first enter: cmd = nil, want the board check")
			}

			cmd, intent := m.Update(tea.KeyMsg{Type: tt.key})
			if cmd != nil || intent != nil {
				t.Fatalf("cmd, intent = %v, %v, want nil, nil while the check is in flight", cmd, intent)
			}
		})
	}
}
