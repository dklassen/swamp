package jobboard

import (
	"encoding/json"
	"fmt"

	"github.com/dklassen/swamp/documents"
)

// ApplicationForm is what a posting's application form asks for (#168):
// whether each document is required, optional or absent, and the custom
// questions to answer. Contact details, location, demographic and
// compliance (EEO) questions are left out, since nothing should draft
// them. Only Greenhouse exposes forms through a supported API; Ashby and
// Lever forms are entered by hand (#167, #184).
type ApplicationForm struct {
	// Documents has the form's requirement for each document type. A type
	// missing from the map is Absent: the form doesn't ask for it.
	Documents map[documents.Type]Requirement `json:"Documents"`
	Questions []Question                     `json:"Questions"`
}

// Question is one custom question on a form. Type is a source-agnostic
// field type: text, long_text, single_select, multi_select or file, or
// the board's own name for anything else. Options are the choices for a
// select.
type Question struct {
	Label    string   `json:"Label"`
	Required bool     `json:"Required"`
	Type     string   `json:"Type"`
	Options  []string `json:"Options,omitempty"`
}

// Requirement is whether a form asks for a document. The zero value is
// Absent, so a document missing from ApplicationForm.Documents reads as
// not asked for.
type Requirement int

const (
	Absent Requirement = iota
	Optional
	Required
)

var requirementNames = [...]string{
	Absent:   "absent",
	Optional: "optional",
	Required: "required",
}

func (r Requirement) String() string {
	if r < 0 || int(r) >= len(requirementNames) {
		return fmt.Sprintf("Requirement(%d)", int(r))
	}
	return requirementNames[r]
}

// MarshalJSON encodes a Requirement as its name, so the stored form and
// stage_prepare's output read "required", not 2.
func (r Requirement) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON is MarshalJSON's inverse, for reading a stored form back.
func (r *Requirement) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	for i, n := range requirementNames {
		if n == name {
			*r = Requirement(i)
			return nil
		}
	}
	return fmt.Errorf("jobboard: unknown requirement %q", name)
}
