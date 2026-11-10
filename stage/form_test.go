package stage

import (
	"encoding/json"
	"testing"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/jobboard"
)

var testForm = jobboard.ApplicationForm{
	Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required, document.CoverLetter: jobboard.Optional},
	Questions: []jobboard.Question{{Label: "LinkedIn Profile", Required: true, Type: "text"}},
}

// The form's JSON is part of the agent contract (SKILL.md reads it), so a
// Go-side rename has to fail a test rather than silently change it.
func TestApplicationForm_JSONShape(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(testForm)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"Documents":{"cover_letter":"optional","resume":"required"},"Questions":[{"Label":"LinkedIn Profile","Required":true,"Type":"text"}]}`
	if string(got) != want {
		t.Errorf("json.Marshal(form) = %s\nwant %s", got, want)
	}
}
