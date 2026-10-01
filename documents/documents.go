// Package documents locates and checks for a given application's
// cover-letter/resume markdown files on the filesystem. The content
// itself is never read by this package -- it's plain markdown meant to
// be consumed directly by an external agent/editor, not by Swamp. See
// decisions.log for why this is filesystem-backed rather than DB
// columns. The default base directory is "assets" (see cmd/swamp/main.go)
// -- only the storage path was renamed, not this package.
package documents

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Each document type's name: the file's base name, and also the value
// store.DocumentType persists in document_reviews.document_type. Renaming
// one means migrating that column too.
const (
	CoverLetterName = "cover_letter"
	ResumeName      = "resume"
)

// Paths holds the resolved, convention-derived filesystem paths for a
// single application's documents.
type Paths struct {
	CoverLetter string
	Resume      string
}

// ForApplication computes the convention-derived paths for an
// application's documents: <base>/<applicationID>/cover_letter.md and
// <base>/<applicationID>/resume.md. No path is ever persisted -- it's
// recomputed from applicationID whenever needed.
func ForApplication(base string, applicationID int64) Paths {
	dir := filepath.Join(base, strconv.FormatInt(applicationID, 10))
	return Paths{
		CoverLetter: filepath.Join(dir, CoverLetterName+".md"),
		Resume:      filepath.Join(dir, ResumeName+".md"),
	}
}

// Doc is a single document's resolved path and whether it exists on
// disk as of the moment it was checked.
type Doc struct {
	Path   string
	Exists bool
}

// Status is an application's cover-letter/resume presence, as of the
// moment Store.Status checked -- see Store.Status.
type Status struct {
	CoverLetter Doc
	Resume      Doc
}

// Store resolves document paths and checks their presence under a fixed
// base directory, so callers (e.g. the TUI) don't need to know the path
// convention or thread a base path around themselves -- on par with how
// store.Store hides SQL/schema details behind method calls. Store, not
// Paths, owns the filesystem I/O: Paths stays a pure value type.
type Store struct {
	base string
}

// NewStore returns a Store rooted at base.
func NewStore(base string) *Store {
	return &Store{base: base}
}

// EnsureDir creates applicationID's document directory if it doesn't
// already exist yet, and returns its resolved paths. Safe to call more
// than once for the same applicationID -- MkdirAll is a no-op when the
// directory is already there.
func (s *Store) EnsureDir(applicationID int64) (Paths, error) {
	paths := ForApplication(s.base, applicationID)
	if err := os.MkdirAll(filepath.Dir(paths.CoverLetter), 0o755); err != nil {
		return Paths{}, err
	}
	return paths, nil
}

// Status returns applicationID's document paths and whether each exists
// on disk, checked via os.Stat.
func (s *Store) Status(applicationID int64) Status {
	paths := ForApplication(s.base, applicationID)
	return Status{
		CoverLetter: Doc{Path: paths.CoverLetter, Exists: fileExists(paths.CoverLetter)},
		Resume:      Doc{Path: paths.Resume, Exists: fileExists(paths.Resume)},
	}
}

// ByName returns the document whose type is named name (CoverLetterName
// or ResumeName, the store.DocumentType string). Any other name is an error, never a
// fallback to one of the documents, which would silently read or write
// the wrong file (RFC 0004).
func (s Status) ByName(name string) (Doc, error) {
	switch name {
	case CoverLetterName:
		return s.CoverLetter, nil
	case ResumeName:
		return s.Resume, nil
	}
	return Doc{}, fmt.Errorf("documents: no document type %q", name)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
