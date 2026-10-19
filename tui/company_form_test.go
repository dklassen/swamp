package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
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

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != formFieldName {
		t.Fatalf("focus after 1 tab = %d, want formFieldName", m.focus)
	}
	if !m.inputs[formFieldName].Focused() {
		t.Fatal("name field should be focused")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != formFieldSourceRef {
		t.Fatalf("focus after 2 tabs = %d, want formFieldSourceRef", m.focus)
	}
	if m.inputs[formFieldName].Focused() {
		t.Fatal("name field should be blurred after tabbing away")
	}
	if !m.inputs[formFieldSourceRef].Focused() {
		t.Fatal("sourceRef field should be focused")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
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

	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if companySources[m.sourceIndex] != jobboard.Greenhouse {
		t.Fatalf("source after right = %q, want greenhouse", companySources[m.sourceIndex])
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if companySources[m.sourceIndex] != jobboard.Lever {
		t.Fatalf("source after second right = %q, want lever", companySources[m.sourceIndex])
	}

	// Wraps back around rather than clamping.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if companySources[m.sourceIndex] != jobboard.Ashby {
		t.Fatalf("source after third right = %q, want ashby (wrapped)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_LeftArrow_OnSourceField_CyclesSourceBackward(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if companySources[m.sourceIndex] != jobboard.Lever {
		t.Fatalf("source after left = %q, want lever (wrapped backward to the last source)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_LeftRightArrow_OffSourceField_DoesNotChangeSource(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // move focus to name field
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})

	if companySources[m.sourceIndex] != jobboard.Ashby {
		t.Fatalf("source = %q, want ashby unchanged (arrow keys should edit the name field's cursor position, not the source)", companySources[m.sourceIndex])
	}
}

func TestCompanyFormModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (empty fields)", cmd, intent)
	}
}

func TestCompanyFormModel_Enter_ValidFields_ReturnsCreateCompanyCmd(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestCompanyFormModel_Enter_CreatesCompanyWithSelectedSource(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	m := newCompanyFormModel(newFormTestSyncer(s))
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // switch source to greenhouse
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme-token")

	cmd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}

	msg := cmd()
	created, ok := msg.(message.CompanyCreatedMsg)
	if !ok {
		t.Fatalf("msg = %T, want message.CompanyCreatedMsg", msg)
	}
	if created.Err != nil {
		t.Fatalf("message.CompanyCreatedMsg.Err = %v, want nil", created.Err)
	}
	if created.Company.Source != jobboard.Greenhouse {
		t.Fatalf("created company Source = %q, want %q", created.Company.Source, jobboard.Greenhouse)
	}

	stored, err := s.Companies.Get(context.Background(), created.Company.ID)
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
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // wraps backward to lever, the last source
	if companySources[m.sourceIndex] != jobboard.Lever {
		t.Fatalf("source after left = %q, want lever", companySources[m.sourceIndex])
	}
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}

	msg := cmd()
	created, ok := msg.(message.CompanyCreatedMsg)
	if !ok {
		t.Fatalf("msg = %T, want message.CompanyCreatedMsg", msg)
	}
	if created.Err != nil {
		t.Fatalf("message.CompanyCreatedMsg.Err = %v, want nil", created.Err)
	}
	if created.Company.Source != jobboard.Lever {
		t.Fatalf("created company Source = %q, want %q", created.Company.Source, jobboard.Lever)
	}

	stored, err := s.Companies.Get(context.Background(), created.Company.ID)
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

	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (whitespace-only name)", cmd, intent)
	}
}

func TestCompanyFormModel_Enter_TrimsSavedName(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	m := newCompanyFormModel(newFormTestSyncer(s))
	m.inputs[formFieldName].SetValue("  Acme  ")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}
	msg := cmd()
	created, ok := msg.(message.CompanyCreatedMsg)
	if !ok {
		t.Fatalf("msg = %T, want message.CompanyCreatedMsg", msg)
	}
	if created.Err != nil {
		t.Fatalf("message.CompanyCreatedMsg.Err = %v, want nil", created.Err)
	}
	if created.Company.Name != "Acme" {
		t.Fatalf("created company Name = %q, want %q (trimmed)", created.Company.Name, "Acme")
	}
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

	cmd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that checks the board and creates the company")
	}
	created, ok := cmd().(message.CompanyCreatedMsg)
	if !ok {
		t.Fatal("msg is not a message.CompanyCreatedMsg")
	}
	if created.Err == nil {
		t.Fatal("message.CompanyCreatedMsg.Err = nil, want an error for a board that doesn't resolve")
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
			if cmd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
				t.Fatal("first enter: cmd = nil, want the board check")
			}

			cmd, intent := m.Update(tea.KeyPressMsg{Code: tt.key})
			if cmd != nil || intent != nil {
				t.Fatalf("cmd, intent = %v, %v, want nil, nil while the check is in flight", cmd, intent)
			}
		})
	}
}
