package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

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

// assertKinds fails unless stack holds exactly want, bottom first.
func assertKinds(t *testing.T, stack *screenStack, want ...message.Screen) {
	t.Helper()
	if diff := cmp.Diff(want, stack.kinds()); diff != "" {
		t.Errorf("kinds mismatch (-want +got):\n%s", diff)
	}
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
	assertKinds(t, stack, message.ScreenCompanyForm)
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
	assertKinds(t, stack, message.ScreenCompanyEdit)
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
	assertKinds(t, stack, message.ScreenPostingDetail)
}

func TestScreenStack_WithNothingPushed_Fails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		op   func(*screenStack) error
	}{
		{name: "pop", op: func(st *screenStack) error { return st.pop() }},
		{name: "replace", op: func(st *screenStack) error { return st.replace(message.ScreenPostingDetail, newStackedFake()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.op(newScreenStack(model.NewMsgRouter())); err == nil {
				t.Errorf("%s with nothing pushed: nil error, want one", tt.name)
			}
		})
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
