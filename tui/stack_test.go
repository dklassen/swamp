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
func assertKinds(t *testing.T, stack *Stack, want ...message.Screen) {
	t.Helper()
	if diff := cmp.Diff(want, stack.Kinds()); diff != "" {
		t.Errorf("kinds mismatch (-want +got):\n%s", diff)
	}
}

func TestStack_Push_PutsTheScreenOnTop(t *testing.T) {
	t.Parallel()
	stack := NewStack()
	under, over := newStackedFake(), newStackedFake()

	stack.Push(message.ScreenCompanyEdit, under)
	stack.Push(message.ScreenFilterSelect, over)

	if stack.Top() != over {
		t.Errorf("top = %v, want the screen pushed last", stack.Top())
	}
	assertKinds(t, stack, message.ScreenCompanyEdit, message.ScreenFilterSelect)
}

func TestStack_Pop_TakesTheTopOff(t *testing.T) {
	t.Parallel()
	stack := NewStack()
	under, over := newStackedFake(), newStackedFake()
	stack.Push(message.ScreenCompanyEdit, under)
	stack.Push(message.ScreenFilterSelect, over)

	popped, err := stack.Pop()
	if err != nil {
		t.Fatalf("pop: %v", err)
	}

	if popped != over || stack.Top() != under {
		t.Errorf("pop gave %v leaving %v on top; want the top, leaving the one beneath", popped, stack.Top())
	}
	assertKinds(t, stack, message.ScreenCompanyEdit)
}

func TestStack_PopWithNothingPushed_Fails(t *testing.T) {
	t.Parallel()
	if _, err := NewStack().Pop(); err == nil {
		t.Error("pop with nothing pushed: nil error, want one")
	}
}

func TestStack_PushingAKindAlreadyOpen_Panics(t *testing.T) {
	t.Parallel()
	stack := NewStack()
	stack.Push(message.ScreenCompanyForm, newStackedFake())

	defer func() {
		if recover() == nil {
			t.Errorf("pushing %v while it's open didn't panic", message.ScreenCompanyForm)
		}
	}()
	stack.Push(message.ScreenCompanyForm, newStackedFake())
}
