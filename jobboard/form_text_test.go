package jobboard

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
)

func TestParseFormText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		previous *ApplicationForm
		want     ApplicationForm
		wantErr  bool
	}{
		{
			name: "documents and questions, * marks required",
			text: "cover_letter: optional\nresume: required\n\nWhy do you want to work here? *\nLinkedIn profile\n",
			want: ApplicationForm{
				Documents: map[documents.Type]Requirement{documents.CoverLetter: Optional, documents.Resume: Required},
				Questions: []Question{
					{Label: "Why do you want to work here?", Required: true},
					{Label: "LinkedIn profile"},
				},
			},
		},
		{
			name: "a document with no line is absent; comments and blank lines are skipped",
			text: "# Documents\nresume: required\n\n  # Questions\n\n",
			want: ApplicationForm{
				Documents: map[documents.Type]Requirement{documents.Resume: Required},
			},
		},
		{
			name: "a line naming no document type is a question, colon or not",
			text: "Salary expectations: CAD*\n",
			want: ApplicationForm{
				Documents: map[documents.Type]Requirement{},
				Questions: []Question{{Label: "Salary expectations: CAD", Required: true}},
			},
		},
		{
			name: "an absent document is left out of the map",
			text: "cover_letter: absent\n",
			want: ApplicationForm{Documents: map[documents.Type]Requirement{}},
		},
		{
			name:    "an unknown requirement is an error",
			text:    "resume: maybe\n",
			wantErr: true,
		},
		{
			name:    "a question that is only * is an error",
			text:    "*\n",
			wantErr: true,
		},
		{
			name: "an unchanged question keeps its fetched type and options",
			text: "Are you authorized to work in Canada? *\nA new question\n",
			previous: &ApplicationForm{Questions: []Question{
				{Label: "Are you authorized to work in Canada?", Required: true, Type: "single_select", Options: []string{"Yes", "No"}},
			}},
			want: ApplicationForm{
				Documents: map[documents.Type]Requirement{},
				Questions: []Question{
					{Label: "Are you authorized to work in Canada?", Required: true, Type: "single_select", Options: []string{"Yes", "No"}},
					{Label: "A new question"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseFormText(tt.text, tt.previous)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseFormText error = %v, want error: %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseFormText mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestFormText_RoundTrips: the text the TUI seeds its editor with parses
// back into the same form, including fields the text doesn't show.
func TestFormText_RoundTrips(t *testing.T) {
	t.Parallel()

	for _, form := range []ApplicationForm{
		{Documents: map[documents.Type]Requirement{}},
		{
			Documents: map[documents.Type]Requirement{documents.CoverLetter: Optional, documents.Resume: Required},
			Questions: []Question{
				{Label: "Why us?", Required: true, Type: "long_text"},
				{Label: "Pronouns", Type: "single_select", Options: []string{"she/her", "he/him", "they/them"}},
				{Label: "Salary expectations: CAD"},
			},
		},
	} {
		text := FormText(form)
		got, err := ParseFormText(text, &form)
		if err != nil {
			t.Fatalf("ParseFormText(FormText(form)): %v\n%s", err, text)
		}
		if diff := cmp.Diff(form, got); diff != "" {
			t.Errorf("round trip mismatch (-want +got):\n%s\ntext:\n%s", diff, text)
		}
	}
}

// TestFormText_ListsEveryDocumentType: even an empty form shows a line per
// document type, so the user sees what can be set.
func TestFormText_ListsEveryDocumentType(t *testing.T) {
	t.Parallel()

	text := FormText(ApplicationForm{})
	for _, documentType := range documents.Types() {
		if !strings.Contains(text, documentType.String()+": absent") {
			t.Errorf("FormText of an empty form has no %q line:\n%s", documentType.String()+": absent", text)
		}
	}
}
