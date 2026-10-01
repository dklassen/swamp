// Package documents owns application documents: the list of document
// types (see Type), and where each application's markdown files live on
// the filesystem and whether they exist. The content itself is never
// read by this package -- it's plain markdown meant to be consumed
// directly by an external agent/editor, not by Swamp. See decisions.log
// for why this is filesystem-backed rather than DB columns. The default
// base directory is "assets" (see cmd/swamp/main.go) -- only the storage
// path was renamed, not this package.
package documents

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Paths holds the resolved, convention-derived filesystem paths for a
// single application's documents.
type Paths struct {
	CoverLetter string
	Resume      string
}

// path is documentType's file for an application: <dir>/<name>.md.
func path(dir string, documentType Type) string {
	return filepath.Join(dir, documentType.String()+".md")
}

func applicationDir(base string, applicationID int64) string {
	return filepath.Join(base, strconv.FormatInt(applicationID, 10))
}

// ForApplication computes the convention-derived paths for an
// application's documents: <base>/<applicationID>/<type name>.md. No
// path is ever persisted -- it's recomputed from applicationID whenever
// needed.
func ForApplication(base string, applicationID int64) Paths {
	dir := applicationDir(base, applicationID)
	return Paths{
		CoverLetter: path(dir, CoverLetter),
		Resume:      path(dir, Resume),
	}
}

// Doc is a single document's resolved path and whether it exists on
// disk as of the moment it was checked.
type Doc struct {
	Path   string
	Exists bool
}

// Status is an application's documents, one per Type, as of the moment
// Store.Status checked. Look one up with Doc.
//
// CoverLetter and Resume duplicate the entries for those two types for
// the screens that still name them; RFC 0004 step 4 removes them.
type Status struct {
	CoverLetter Doc
	Resume      Doc
	docs        map[Type]Doc
}

// Doc returns documentType's document. A type with no document is an
// error, never a fallback to another document, which would silently read
// or write the wrong file (RFC 0004).
func (s Status) Doc(documentType Type) (Doc, error) {
	doc, ok := s.docs[documentType]
	if !ok {
		return Doc{}, fmt.Errorf("documents: no %s document", documentType)
	}
	return doc, nil
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
	if err := os.MkdirAll(applicationDir(s.base, applicationID), 0o755); err != nil {
		return Paths{}, err
	}
	return ForApplication(s.base, applicationID), nil
}

// Path is where applicationID's documentType document lives, whether or
// not it exists yet. An unknown type is an error.
func (s *Store) Path(applicationID int64, documentType Type) (string, error) {
	if !documentType.valid() {
		return "", fmt.Errorf("documents: no %s document", documentType)
	}
	return path(applicationDir(s.base, applicationID), documentType), nil
}

// Status returns applicationID's documents, one per Type, and whether
// each exists on disk, checked via os.Stat.
func (s *Store) Status(applicationID int64) Status {
	dir := applicationDir(s.base, applicationID)
	docs := make(map[Type]Doc, len(types))
	for _, documentType := range Types() {
		p := path(dir, documentType)
		docs[documentType] = Doc{Path: p, Exists: fileExists(p)}
	}
	return Status{CoverLetter: docs[CoverLetter], Resume: docs[Resume], docs: docs}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
