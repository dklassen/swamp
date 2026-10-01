package documents

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Adding a document type is one entry in the types table. This fails if
// an entry is missing its name or label, or has no document (RFC 0004).
func TestTypes_EachHasANameLabelAndDocument(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	status := store.Status(7)
	if len(Types()) == 0 {
		t.Fatal("Types() is empty")
	}
	seen := map[string]bool{}
	for _, documentType := range Types() {
		name := documentType.String()
		if name == "" || strings.HasPrefix(name, "Type(") {
			t.Errorf("type %d has no name", int(documentType))
		}
		if seen[name] {
			t.Errorf("name %q is used twice", name)
		}
		seen[name] = true
		if documentType.Label() == "" {
			t.Errorf("%s has no label", name)
		}
		doc, err := status.Doc(documentType)
		if err != nil {
			t.Errorf("%s: Doc: %v", name, err)
			continue
		}
		if filepath.Base(doc.Path) != name+".md" {
			t.Errorf("%s: path %s, want a file named %s.md", name, doc.Path, name)
		}
		if p, err := store.Path(7, documentType); err != nil || p != doc.Path {
			t.Errorf("%s: Path = %q, %v; want %q", name, p, err, doc.Path)
		}
		parsed, err := ParseType(name)
		if err != nil || parsed != documentType {
			t.Errorf("ParseType(%q) = %v, %v; want %v", name, parsed, err, documentType)
		}
	}
}

// An unknown type is an error, never a fallback to another document.
func TestUnknownType_IsAnError(t *testing.T) {
	t.Parallel()
	if _, err := ParseType("answers"); err == nil {
		t.Error(`ParseType("answers") succeeded, want an error`)
	}
	if _, err := NewStore(t.TempDir()).Status(7).Doc(Type(len(Types()))); err == nil {
		t.Error("Doc of an out-of-range Type succeeded, want an error")
	}
	if _, err := NewStore(t.TempDir()).Path(7, Type(len(Types()))); err == nil {
		t.Error("Path of an out-of-range Type succeeded, want an error")
	}
}

// A map keyed by Type serializes its keys as names, not ints: the agent
// reads LatestReviews this way (encoding/json uses MarshalText for map
// keys, not MarshalJSON).
func TestType_JSONMapKeysAreNames(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(map[Type]bool{CoverLetter: true, Resume: false})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if want := `{"cover_letter":true,"resume":false}`; string(got) != want {
		t.Errorf("json.Marshal = %s, want %s", got, want)
	}
}
