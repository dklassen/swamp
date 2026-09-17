package export_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dklassen/swamp/export"
	"github.com/dklassen/swamp/store"
)

// writeMarkdown writes minimal valid markdown to a temp file and returns
// its path -- the input every Document test needs before it can export
// anything.
func writeMarkdown(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cover_letter.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	return path
}

func TestDocument_WritesPDFToDestination(t *testing.T) {
	t.Parallel()

	mdPath := writeMarkdown(t, "# Cover letter\n\nHello.\n")
	outPath := filepath.Join(t.TempDir(), "acme-engineer-cover_letter.pdf")

	if err := export.Document(mdPath, outPath); err != nil {
		t.Fatalf("Document() error = %v, want nil", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read exported pdf: %v", err)
	}
	if len(got) < 4 || string(got[:4]) != "%PDF" {
		t.Fatalf("exported file does not start with %%PDF (len %d)", len(got))
	}
}

func TestFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		company      string
		title        string
		documentType store.DocumentType
		want         string
	}{
		{
			name:         "lowercases and hyphenates",
			company:      "WealthSimple",
			title:        "Delivery Platform",
			documentType: store.DocumentTypeCoverLetter,
			want:         "wealthsimple-delivery-platform-cover_letter.pdf",
		},
		{
			name:         "resume keeps its own document-type suffix",
			company:      "StackAdapt",
			title:        "Integrations",
			documentType: store.DocumentTypeResume,
			want:         "stackadapt-integrations-resume.pdf",
		},
		{
			name:         "collapses punctuation runs into single hyphens",
			company:      "WealthSimple",
			title:        "Sr Data Scientist, Finance, Brokerage & Market Risk",
			documentType: store.DocumentTypeResume,
			want:         "wealthsimple-sr-data-scientist-finance-brokerage-market-risk-resume.pdf",
		},
		{
			name:         "trims leading and trailing separators",
			company:      "  Acme!  ",
			title:        "(Senior) Engineer -- Infra/Platform",
			documentType: store.DocumentTypeCoverLetter,
			want:         "acme-senior-engineer-infra-platform-cover_letter.pdf",
		},
		{
			name:         "omits an empty component rather than leaving a stray hyphen",
			company:      "",
			title:        "Engineer",
			documentType: store.DocumentTypeResume,
			want:         "engineer-resume.pdf",
		},
		{
			name:         "falls back to the document type when nothing else survives slugging",
			company:      "!!!",
			title:        "",
			documentType: store.DocumentTypeResume,
			want:         "resume.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := export.FileName(tt.company, tt.title, tt.documentType); got != tt.want {
				t.Errorf("FileName(%q, %q, %s) = %q, want %q", tt.company, tt.title, tt.documentType, got, tt.want)
			}
		})
	}
}
