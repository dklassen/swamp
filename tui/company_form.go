package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/model"
)

// companySources are the company.Company.Source values this form can
// create, in the order the source field cycles through.
var companySources = jobboard.Sources()

// sourceRefLabels gives each source's second field a label matching what
// that source actually needs to identify a company's board.
var sourceRefLabels = map[string]string{
	jobboard.Ashby:      "Ashby slug",
	jobboard.Greenhouse: "Greenhouse board token",
	jobboard.Lever:      "Lever site",
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
	// would race the save (as on the edit screen, #37) and a
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

func (m *companyFormModel) Update(msg tea.KeyPressMsg) (tea.Cmd, tea.Msg) {
	if m.checking {
		return nil, nil
	}
	switch {
	case msg.String() == "esc":
		return nil, cancelCompanyFormMsg{}
	case msg.String() == "tab":
		m.blurFocused()
		m.focus = (m.focus + 1) % formFieldCount
		m.focusFocused()
		return nil, nil
	case m.focus == formFieldSource && msg.String() == "right":
		m.cycleSource(1)
		return nil, nil
	case m.focus == formFieldSource && msg.String() == "left":
		m.cycleSource(-1)
		return nil, nil
	case msg.String() == "enter":
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
// by App once message.CompanyCreatedMsg arrives, success or failure -- on
// failure the form stays open with its inputs intact so a typo can be
// fixed.
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
			parts[i] = model.CursorStyle.Render("[" + src + "]")
		} else {
			parts[i] = " " + src + " "
		}
	}
	return strings.Join(parts, "  ")
}

func (m *companyFormModel) View() string {
	var b strings.Builder

	sourceLabel := model.FieldLabel
	if m.focus == formFieldSource {
		sourceLabel = model.FocusedLabel
	}
	b.WriteString(sourceLabel.Render("Source:") + " " + renderSourcePicker(m.sourceIndex) + "\n")

	labels := map[int]string{
		formFieldName:      "Name",
		formFieldSourceRef: sourceRefLabels[companySources[m.sourceIndex]],
	}
	for _, field := range []int{formFieldName, formFieldSourceRef} {
		label := model.FieldLabel
		if field == m.focus {
			label = model.FocusedLabel
		}
		b.WriteString(label.Render(labels[field]+":") + " " + m.inputs[field].View() + "\n")
	}

	if m.checking {
		b.WriteString(model.HelpStyle.Render("Checking board...") + "\n")
	}
	b.WriteString(model.HelpStyle.Render("tab: next field  ←/→: change source  enter: save  esc: cancel"))
	return b.String()
}

func (m *companyFormModel) paste(msg tea.PasteMsg) tea.Cmd {
	if m.checking || m.focus == formFieldSource {
		return nil
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return cmd
}
