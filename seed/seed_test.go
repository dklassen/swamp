package seed

import (
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse_ValidFile(t *testing.T) {
	input := `
companies:
  - name: Initech
    source: greenhouse
    source_ref: initech
  - name: Hooli
    source: ashby
    source_ref: hooli
`
	got, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []Entry{
		{Name: "Initech", Source: "greenhouse", SourceRef: "initech"},
		{Name: "Hooli", Source: "ashby", SourceRef: "hooli"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Parse() mismatch (-want +got):\n%s", diff)
	}
}

func TestParse_MissingField(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "missing name",
			input: `
companies:
  - source: greenhouse
    source_ref: initech
`,
		},
		{
			name: "missing source",
			input: `
companies:
  - name: Initech
    source_ref: initech
`,
		},
		{
			name: "missing source_ref",
			input: `
companies:
  - name: Initech
    source: greenhouse
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("Parse() = nil error, want error for missing field")
			}
		})
	}
}

func TestParse_MalformedYAML(t *testing.T) {
	input := `companies: [this is not a valid company list`

	_, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("Parse() = nil error, want error for malformed YAML")
	}
}

// description is optional: entries with one carry it through, entries
// without one are still valid.
func TestParse_OptionalDescription(t *testing.T) {
	input := `
companies:
  - name: Initech
    source: greenhouse
    source_ref: initech
    description: Payments infrastructure for the internet.
  - name: Hooli
    source: ashby
    source_ref: hooli
`
	got, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []Entry{
		{Name: "Initech", Source: "greenhouse", SourceRef: "initech", Description: "Payments infrastructure for the internet."},
		{Name: "Hooli", Source: "ashby", SourceRef: "hooli"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Parse() mismatch (-want +got):\n%s", diff)
	}
}

// TestParse_ExampleFile: the committed example, which shows the format
// without real company names, stays a file `swamp import` accepts.
func TestParse_ExampleFile(t *testing.T) {
	f, err := os.Open("data/companies.example.yaml")
	if err != nil {
		t.Fatalf("open example: %v", err)
	}
	defer func() { _ = f.Close() }()

	entries, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse(example): %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("example has no companies")
	}
}
