package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// probeMsg is a message only probe registers for.
type probeMsg struct{}

// probe registers for probeMsg in both scopes and counts what reaches it.
type probe struct{ always, focused int }

func (p *probe) Init() tea.Cmd                       { return nil }
func (p *probe) Update(tea.Msg) (tea.Model, tea.Cmd) { return p, nil }
func (p *probe) View() tea.View                      { return tea.NewView("") }
func (p *probe) RegisterRoutes(r model.Router) error {
	return errors.Join(
		r.Register(probeMsg{}, p, model.AlwaysScope, func(tea.Msg) tea.Cmd { p.always++; return nil }),
		r.Register(probeMsg{}, p, model.FocusScope, func(tea.Msg) tea.Cmd { p.focused++; return nil }),
	)
}

// A component that isn't in focus gets what it registered for always, and
// nothing it registered for only while in focus.
func TestApp_Route_DeliversByScope(t *testing.T) {
	t.Parallel()
	p := &probe{}

	app := newTestApp(t, testutil.NewTestApp(t), nil)

	sendKey(app, probeMsg{})

	if p.always != 1 || p.focused != 0 {
		t.Errorf("deliveries: always %d, focused %d; want 1 and 0 (the probe is never in focus)", p.always, p.focused)
	}
}

// A key reaches the company list once while it's on screen: one down
// moves one row, so enter opens the second company, not the third.
func TestApp_CompanyList_GetsEachKeyOnce(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	globex := mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	mustCreateCompany(t, s, "Initech", jobboard.Ashby, "initech")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("enter returned no command, want message.OpenCompanyPostingsMsg")
	}
	msg := cmd()
	if got, ok := msg.(message.OpenCompanyPostingsMsg); !ok || got.Company.ID != globex.ID {
		t.Errorf("enter after one down = %#v, want message.OpenCompanyPostingsMsg for Globex (ID %d)", msg, globex.ID)
	}
}

// An ErrMsg is the program's fault, not something the user can act on:
// the banner says where to look, not what went wrong.
func TestApp_ErrMsg_ShowsTheInternalErrorBanner(t *testing.T) {
	t.Parallel()
	app := newTestApp(t, testutil.NewTestApp(t), nil)

	app, _ = sendKey(app, message.ErrMsg{Err: errors.New("handler wiring broke")})

	view := viewText(app)
	if !strings.Contains(view, errInternal.Error()) {
		t.Errorf("view after ErrMsg doesn't show %q:\n%s", errInternal.Error(), view)
	}
	if strings.Contains(view, "handler wiring broke") {
		t.Errorf("view after ErrMsg shows the wrapped error:\n%s", view)
	}
}
