package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

type stackPing struct{}

// stackedFake is a screen that counts the pings it hears.
type stackedFake struct {
	instance message.ScreenInstance
	heard    int
}

func newStackedFake() *stackedFake {
	return &stackedFake{instance: message.NewScreenInstance()}
}

func (f *stackedFake) Init() tea.Cmd                       { return nil }
func (f *stackedFake) Update(tea.Msg) (tea.Model, tea.Cmd) { return f, nil }
func (f *stackedFake) View() tea.View                      { return tea.NewView("") }
func (f *stackedFake) Instance() message.ScreenInstance    { return f.instance }
func (f *stackedFake) Routes() []*model.Route {
	return []*model.Route{model.Always(func(stackPing) tea.Cmd { f.heard++; return nil })}
}

func TestScreenStack_Push_RoutesToTheScreen(t *testing.T) {
	t.Parallel()
	router := model.NewMsgRouter()
	stack := newScreenStack(router)
	screen := newStackedFake()

	if err := stack.push(message.ScreenCompanyForm, screen, screenCompanyList); err != nil {
		t.Fatalf("push: %v", err)
	}
	router.Dispatch(stackPing{})

	if screen.heard != 1 {
		t.Errorf("pings the pushed screen heard = %d, want 1", screen.heard)
	}
	if stack.top() != screen {
		t.Errorf("top = %v, want the pushed screen", stack.top())
	}
}

func TestScreenStack_Pop_StopsTheScreensRoutesAndReturnsWhatItCovered(t *testing.T) {
	t.Parallel()
	router := model.NewMsgRouter()
	stack := newScreenStack(router)
	under, over := newStackedFake(), newStackedFake()
	if err := stack.push(message.ScreenCompanyEdit, under, screenCompanyList); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := stack.push(message.ScreenCompanyDelete, over, screenCompanyEdit); err != nil {
		t.Fatalf("push: %v", err)
	}

	covered, err := stack.pop()
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	router.Dispatch(stackPing{})

	if covered != screenCompanyEdit {
		t.Errorf("pop returned %v, want %v, the screen it was pushed over", covered, screenCompanyEdit)
	}
	if over.heard != 0 || under.heard != 1 {
		t.Errorf("pings heard after the pop: popped %d, beneath %d; want 0 and 1", over.heard, under.heard)
	}
	if stack.top() != under {
		t.Errorf("top after the pop = %v, want the screen beneath", stack.top())
	}
}

func TestScreenStack_PopWithNothingPushed_Fails(t *testing.T) {
	t.Parallel()
	stack := newScreenStack(model.NewMsgRouter())

	if _, err := stack.pop(); err == nil {
		t.Error("pop with nothing pushed: nil error, want one")
	}
}

func TestScreenStack_PushingAKindAlreadyOpen_Panics(t *testing.T) {
	t.Parallel()
	stack := newScreenStack(model.NewMsgRouter())
	if err := stack.push(message.ScreenCompanyForm, newStackedFake(), screenCompanyList); err != nil {
		t.Fatalf("push: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Errorf("pushing %v while it's open didn't panic", message.ScreenCompanyForm)
		}
	}()
	_ = stack.push(message.ScreenCompanyForm, newStackedFake(), screenCompanyForm)
}
