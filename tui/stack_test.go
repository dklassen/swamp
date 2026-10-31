package tui

import (
	"slices"
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

// assertMoved fails unless stack moved exactly the screens closed and
// opened, the same instances.
func assertMoved(t *testing.T, stack *Stack, closed, opened []stackScreen) {
	t.Helper()
	gotClosed, gotOpened := stack.Moved()
	if !slices.Equal(gotClosed, closed) || !slices.Equal(gotOpened, opened) {
		t.Errorf("moved: closed %v, opened %v; want %v and %v", gotClosed, gotOpened, closed, opened)
	}
}

func TestStack_Push_OpensTheScreen(t *testing.T) {
	t.Parallel()
	stack := NewStack()
	screen := newStackedFake()

	stack.push(message.ScreenCompanyForm, screen)

	assertKinds(t, stack, message.ScreenCompanyForm)
	assertMoved(t, stack, nil, []stackScreen{screen})
}

func TestStack_Pop_ClosesTheTop(t *testing.T) {
	t.Parallel()
	stack := NewStack()
	under, over := newStackedFake(), newStackedFake()
	stack.push(message.ScreenCompanyEdit, under)
	stack.push(message.ScreenFilterSelect, over)
	stack.Moved()

	if err := stack.pop(); err != nil {
		t.Fatalf("pop: %v", err)
	}

	assertKinds(t, stack, message.ScreenCompanyEdit)
	assertMoved(t, stack, []stackScreen{over}, nil)
}

func TestStack_Replace_SwapsTheTopOverTheSameScreen(t *testing.T) {
	t.Parallel()
	stack := NewStack()
	old, replacement := newStackedFake(), newStackedFake()
	stack.push(message.ScreenPostingDetail, old)
	stack.Moved()

	if err := stack.replace(message.ScreenPostingDetail, replacement); err != nil {
		t.Fatalf("replace: %v", err)
	}

	assertKinds(t, stack, message.ScreenPostingDetail)
	assertMoved(t, stack, []stackScreen{old}, []stackScreen{replacement})
}

func TestStack_WithNothingPushed_Fails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		op   func(*Stack) error
	}{
		{name: "pop", op: func(st *Stack) error { return st.pop() }},
		{name: "replace", op: func(st *Stack) error { return st.replace(message.ScreenPostingDetail, newStackedFake()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.op(NewStack()); err == nil {
				t.Errorf("%s with nothing pushed: nil error, want one", tt.name)
			}
		})
	}
}

func TestStack_PushingAKindAlreadyOpen_Panics(t *testing.T) {
	t.Parallel()
	stack := NewStack()
	stack.push(message.ScreenCompanyForm, newStackedFake())

	defer func() {
		if recover() == nil {
			t.Errorf("pushing %v while it's open didn't panic", message.ScreenCompanyForm)
		}
	}()
	stack.push(message.ScreenCompanyForm, newStackedFake())
}
