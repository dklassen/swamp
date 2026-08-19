// Package documents locates and checks for a given application's
// cover-letter/resume markdown files on the filesystem. The content
// itself is never read by this package -- it's plain markdown meant to
// be consumed directly by an external agent/editor, not by Swamp. See
// decisions.log for why this is filesystem-backed rather than DB
// columns.
package documents

import (
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

// ForApplication computes the convention-derived paths for an
// application's documents: <base>/<applicationID>/cover_letter.md and
// <base>/<applicationID>/resume.md. No path is ever persisted -- it's
// recomputed from applicationID whenever needed.
func ForApplication(base string, applicationID int64) Paths {
	dir := filepath.Join(base, strconv.FormatInt(applicationID, 10))
	return Paths{
		CoverLetter: filepath.Join(dir, "cover_letter.md"),
		Resume:      filepath.Join(dir, "resume.md"),
	}
}

// Store resolves document paths under a fixed base directory, so callers
// (e.g. the TUI) don't need to know the path convention or thread a base
// path around themselves -- on par with how store.Store hides SQL/schema
// details behind method calls.
type Store struct {
	base string
}

// NewStore returns a Store rooted at base.
func NewStore(base string) *Store {
	return &Store{base: base}
}

// Get returns applicationID's document paths.
func (s *Store) Get(applicationID int64) Paths {
	return ForApplication(s.base, applicationID)
}

// CoverLetterExists reports whether the cover letter file exists on
// disk.
func (p Paths) CoverLetterExists() bool {
	return fileExists(p.CoverLetter)
}

// ResumeExists reports whether the resume file exists on disk.
func (p Paths) ResumeExists() bool {
	return fileExists(p.Resume)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
