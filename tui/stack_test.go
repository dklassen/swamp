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

	if err := stack.push(message.ScreenCompanyForm, screen); err != nil {
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

func TestScreenStack_Pop_StopsTheScreensRoutes(t *testing.T) {
	t.Parallel()
	router := model.NewMsgRouter()
	stack := newScreenStack(router)
	under, over := newStackedFake(), newStackedFake()
	if err := stack.push(message.ScreenCompanyEdit, under); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := stack.push(message.ScreenCompanyDelete, over); err != nil {
		t.Fatalf("push: %v", err)
	}

	if err := stack.pop(); err != nil {
		t.Fatalf("pop: %v", err)
	}
	router.Dispatch(stackPing{})

	if over.heard != 0 || under.heard != 1 {
		t.Errorf("pings heard after the pop: popped %d, beneath %d; want 0 and 1", over.heard, under.heard)
	}
	if stack.top() != under {
		t.Errorf("top after the pop = %v, want the screen beneath", stack.top())
	}
}

func TestScreenStack_Replace_SwapsTheTopOverTheSameScreen(t *testing.T) {
	t.Parallel()
	router := model.NewMsgRouter()
	stack := newScreenStack(router)
	old, replacement := newStackedFake(), newStackedFake()
	if err := stack.push(message.ScreenPostingDetail, old); err != nil {
		t.Fatalf("push: %v", err)
	}

	if err := stack.replace(message.ScreenPostingDetail, replacement); err != nil {
		t.Fatalf("replace: %v", err)
	}
	router.Dispatch(stackPing{})

	if old.heard != 0 || replacement.heard != 1 {
		t.Errorf("pings heard after the replace: replaced %d, replacement %d; want 0 and 1", old.heard, replacement.heard)
	}
	if stack.top() != replacement || stack.len() != 1 {
		t.Errorf("top = %v of %d, want the replacement alone", stack.top(), stack.len())
	}
}

func TestScreenStack_ReplaceWithNothingPushed_Fails(t *testing.T) {
	t.Parallel()
	stack := newScreenStack(model.NewMsgRouter())

	if err := stack.replace(message.ScreenPostingDetail, newStackedFake()); err == nil {
		t.Error("replace with nothing pushed: nil error, want one")
	}
}

func TestScreenStack_PopWithNothingPushed_Fails(t *testing.T) {
	t.Parallel()
	stack := newScreenStack(model.NewMsgRouter())

	if err := stack.pop(); err == nil {
		t.Error("pop with nothing pushed: nil error, want one")
	}
}

func TestScreenStack_PushingAKindAlreadyOpen_Panics(t *testing.T) {
	t.Parallel()
	stack := newScreenStack(model.NewMsgRouter())
	if err := stack.push(message.ScreenCompanyForm, newStackedFake()); err != nil {
		t.Fatalf("push: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Errorf("pushing %v while it's open didn't panic", message.ScreenCompanyForm)
		}
	}()
	_ = stack.push(message.ScreenCompanyForm, newStackedFake())
}
