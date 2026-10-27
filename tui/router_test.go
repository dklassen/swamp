package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
)

// probeMsg is a message nothing registers for.
type probeMsg struct{}

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

// A fault is the program's, not something the user can act on: the
// banner says where to look, not what went wrong.
func TestApp_Fault_ShowsTheInternalErrorBanner(t *testing.T) {
	t.Parallel()
	app := newTestApp(t, testutil.NewTestApp(t), nil)

	app, _ = sendKey(app, message.Fail(message.Source{}, errors.New("handler wiring broke"))())

	view := viewText(app)
	if !strings.Contains(view, errInternal.Error()) {
		t.Errorf("view after a fault doesn't show %q:\n%s", errInternal.Error(), view)
	}
	if strings.Contains(view, "handler wiring broke") {
		t.Errorf("view after a fault shows the wrapped error:\n%s", view)
	}
}
