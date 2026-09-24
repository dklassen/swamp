package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/sync"
)

// companySources are the store.Company.Source values this form can
// create, in the order the source field cycles through. Must be kept in
// sync with cmd/swamp/main.go's newSyncer -- a source added there with no
// entry here can never actually be added as a company.
var companySources = []string{"ashby", "greenhouse", "lever"}

// sourceRefLabels gives each source's second field a label matching what
// that source actually needs to identify a company's board.
var sourceRefLabels = map[string]string{
	"ashby":      "Ashby slug",
	"greenhouse": "Greenhouse board token",
	"lever":      "Lever site",
}

// formInputs indices: a non-textinput source picker, then name, then
// source ref (whose label depends on the selected source -- see
// sourceRefLabels). inputs is sized to formFieldCount even though
// formFieldSource has no textinput of its own, so formFieldName/
// formFieldSourceRef index into it directly without an offset.
const (
	formFieldSource = iota
	formFieldName
	formFieldSourceRef
	formFieldCount
)

// companyFormModel drives the add-company screen. It holds the syncer it
// needs to dispatch createCompany (which checks the board before
// saving), and the form's own private input/focus state -- no other
// screen reads or writes this state.
type companyFormModel struct {
	syncer      *sync.Syncer
	sourceIndex int
	inputs      []textinput.Model
	focus       int
	// checking is true from the moment Enter dispatches createCompany
	// until checkResolved is called with its result. The form ignores
	// keys meanwhile: bubbletea can't cancel a dispatched command, so Esc
	// would race the save (as on the edit screen, decisions.log #37) and a
	// second Enter would start a duplicate check.
	checking bool
}

// newCompanyFormModel returns a fresh, blank form with the source picker
// focused first (defaulting to companySources[0]) -- constructed anew
// each time the screen is entered, replacing the previous reset-in-place
// pattern (App.formInputs[i].SetValue("")).
func newCompanyFormModel(syncer *sync.Syncer) companyFormModel {
	inputs := make([]textinput.Model, formFieldCount)
	for i := range inputs {
		inputs[i] = textinput.New()
	}
	return companyFormModel{syncer: syncer, inputs: inputs}
}

// cancelCompanyFormMsg signals that App should switch back to the
// company-list screen without creating anything.
type cancelCompanyFormMsg struct{}

// blurFocused/focusFocused are no-ops when the source picker is focused
// -- it has no textinput of its own to blur/focus.
func (m *companyFormModel) blurFocused() {
	if m.focus != formFieldSource {
		m.inputs[m.focus].Blur()
	}
}

func (m *companyFormModel) focusFocused() {
	if m.focus != formFieldSource {
		m.inputs[m.focus].Focus()
	}
}

func (m *companyFormModel) cycleSource(direction int) {
	n := len(companySources)
	m.sourceIndex = ((m.sourceIndex+direction)%n + n) % n
}

func (m *companyFormModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	if m.checking {
		return nil, nil
	}
	switch {
	case msg.Type == tea.KeyEsc:
		return nil, cancelCompanyFormMsg{}
	case msg.Type == tea.KeyTab:
		m.blurFocused()
		m.focus = (m.focus + 1) % formFieldCount
		m.focusFocused()
		return nil, nil
	case m.focus == formFieldSource && msg.Type == tea.KeyRight:
		m.cycleSource(1)
		return nil, nil
	case m.focus == formFieldSource && msg.Type == tea.KeyLeft:
		m.cycleSource(-1)
		return nil, nil
	case msg.Type == tea.KeyEnter:
		name := strings.TrimSpace(m.inputs[formFieldName].Value())
		sourceRef := m.inputs[formFieldSourceRef].Value()
		if name == "" || sourceRef == "" {
			return nil, nil
		}
		m.checking = true
		return createCompany(m.syncer, name, companySources[m.sourceIndex], sourceRef), nil
	}
	if m.focus == formFieldSource {
		return nil, nil
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return cmd, nil
}

// checkResolved marks the in-flight board check (if any) as done. Called
// by App once companyCreatedMsg arrives, success or failure -- on failure
// the form stays open with its inputs intact so a typo can be fixed.
func (m *companyFormModel) checkResolved() {
	m.checking = false
}

// renderSourcePicker shows every source with the selected one boxed and
// highlighted, so both options (and which is active) are always visible
// without needing to cycle through them first.
func renderSourcePicker(selected int) string {
	parts := make([]string, len(companySources))
	for i, src := range companySources {
		if i == selected {
			parts[i] = cursorStyle.Render("[" + src + "]")
		} else {
			parts[i] = " " + src + " "
		}
	}
	return strings.Join(parts, "  ")
}

func (m *companyFormModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Add company") + "\n")

	sourceLabel := fieldLabel
	if m.focus == formFieldSource {
		sourceLabel = focusedLabel
	}
	b.WriteString(sourceLabel.Render("Source:") + " " + renderSourcePicker(m.sourceIndex) + "\n")

	labels := map[int]string{
		formFieldName:      "Name",
		formFieldSourceRef: sourceRefLabels[companySources[m.sourceIndex]],
	}
	for _, field := range []int{formFieldName, formFieldSourceRef} {
		label := fieldLabel
		if field == m.focus {
			label = focusedLabel
		}
		b.WriteString(label.Render(labels[field]+":") + " " + m.inputs[field].View() + "\n")
	}

	if m.checking {
		b.WriteString(helpStyle.Render("Checking board...") + "\n")
	}
	b.WriteString(helpStyle.Render("tab: next field  ←/→: change source  enter: save  esc: cancel"))
	return b.String()
}
