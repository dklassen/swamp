package jobboard

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dklassen/swamp/internal/domain/document"
)

// ParseFormText reads an application form entered by hand (#184), for
// boards whose forms Swamp can't fetch (Ashby, Lever). One line per
// document, "<type name>: required|optional|absent", where a document
// with no line is absent. Every other non-blank line is a question; a
// trailing "*" marks it required, as apply pages usually do, so pasted
// labels keep their marking. Lines starting with "#" are comments.
//
// A line counts as a document line only if it names a document type, so
// a question with a colon in it ("Salary expectations: CAD") stays a
// question.
//
// previous is the form being edited, if any: a question whose label is
// unchanged keeps its Type and Options, which the text doesn't show. A
// question that's new has neither.
func ParseFormText(text string, previous *ApplicationForm) (ApplicationForm, error) {
	known := map[string]Question{}
	if previous != nil {
		for _, q := range previous.Questions {
			known[q.Label] = q
		}
	}

	form := ApplicationForm{Documents: map[document.Type]Requirement{}}
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if documentType, value, ok := documentLine(line); ok {
			requirement, err := parseRequirement(value)
			if err != nil {
				return ApplicationForm{}, fmt.Errorf("jobboard: line %d: %w", n+1, err)
			}
			if requirement == Absent {
				delete(form.Documents, documentType)
			} else {
				form.Documents[documentType] = requirement
			}
			continue
		}

		label, required := strings.CutSuffix(line, "*")
		label = strings.TrimSpace(label)
		if label == "" {
			return ApplicationForm{}, fmt.Errorf("jobboard: line %d: %w", n+1, errEmptyQuestion)
		}
		question := Question{Label: label, Required: required}
		if before, ok := known[label]; ok {
			question.Type = before.Type
			question.Options = before.Options
		}
		form.Questions = append(form.Questions, question)
	}
	return form, nil
}

// documentLine is the document type a "<type name>: <value>" line names,
// and its value, or false if the line names none.
func documentLine(line string) (document.Type, string, bool) {
	name, value, ok := strings.Cut(line, ":")
	if !ok {
		return 0, "", false
	}
	documentType, err := document.ParseType(strings.TrimSpace(name))
	if err != nil {
		return 0, "", false
	}
	return documentType, strings.TrimSpace(value), true
}

var errEmptyQuestion = errors.New("a question needs a label")

// FormText writes form in the text ParseFormText reads, for editing by
// hand: a line for every document type, absent ones included, then the
// questions, with short comments saying how to fill it in.
func FormText(form ApplicationForm) string {
	var b strings.Builder
	b.WriteString("# Documents: required, optional or absent\n")
	for _, documentType := range document.Types() {
		fmt.Fprintf(&b, "%s: %s\n", documentType, form.Documents[documentType])
	}
	b.WriteString("\n# Questions, one per line. End a line with * if it's required.\n")
	for _, q := range form.Questions {
		b.WriteString(q.Label)
		if q.Required {
			b.WriteString(" *")
		}
		b.WriteString("\n")
	}
	return b.String()
}
