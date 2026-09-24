package seed

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse_ValidFile(t *testing.T) {
	input := `
companies:
  - name: Stripe
    source: greenhouse
    source_ref: stripe
  - name: Cohere
    source: ashby
    source_ref: cohere
`
	got, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []Entry{
		{Name: "Stripe", Source: "greenhouse", SourceRef: "stripe"},
		{Name: "Cohere", Source: "ashby", SourceRef: "cohere"},
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
    source_ref: stripe
`,
		},
		{
			name: "missing source",
			input: `
companies:
  - name: Stripe
    source_ref: stripe
`,
		},
		{
			name: "missing source_ref",
			input: `
companies:
  - name: Stripe
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
  - name: Stripe
    source: greenhouse
    source_ref: stripe
    description: Payments infrastructure for the internet.
  - name: Cohere
    source: ashby
    source_ref: cohere
`
	got, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []Entry{
		{Name: "Stripe", Source: "greenhouse", SourceRef: "stripe", Description: "Payments infrastructure for the internet."},
		{Name: "Cohere", Source: "ashby", SourceRef: "cohere"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Parse() mismatch (-want +got):\n%s", diff)
	}
}
