// Package documents owns application documents: the list of document
// types (see Type), and where each application's markdown files live on
// the filesystem and whether they exist. Content is read only to check
// whether a review or export still matches it (see Current) -- it's plain
// markdown meant to be consumed directly by an external agent/editor,
// not by Swamp. See decisions.log
// for why this is filesystem-backed rather than DB columns. The default
// base directory is "assets" (see cmd/swamp/main.go) -- only the storage
// path was renamed, not this package.
package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// path is documentType's file for an application: <dir>/<name>.md.
func path(dir string, documentType Type) string {
	return filepath.Join(dir, documentType.String()+".md")
}

func applicationDir(base string, applicationID int64) string {
	return filepath.Join(base, strconv.FormatInt(applicationID, 10))
}

// Doc is a single document's resolved path and whether it exists on
// disk as of the moment it was checked.
type Doc struct {
	Path   string
	Exists bool
}

// Status is an application's documents, one per Type, as of the moment
// Store.Status checked. Look one up with Doc.
type Status struct {
	docs map[Type]Doc
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
// store.Store hides SQL/schema details behind method calls. Store owns the
// filesystem I/O.
type Store struct {
	base string
}

// NewStore returns a Store rooted at base.
func NewStore(base string) *Store {
	return &Store{base: base}
}

// EnsureDir creates applicationID's document directory if it doesn't
// already exist yet, and returns its documents. Safe to call more than
// once for the same applicationID -- MkdirAll is a no-op when the
// directory is already there.
func (s *Store) EnsureDir(applicationID int64) (Status, error) {
	if err := os.MkdirAll(applicationDir(s.base, applicationID), 0o755); err != nil {
		return Status{}, err
	}
	return s.Status(applicationID), nil
}

// Path is where applicationID's documentType document lives, whether or
// not it exists yet. An unknown type is an error.
func (s *Store) Path(applicationID int64, documentType Type) (string, error) {
	if !documentType.valid() {
		return "", fmt.Errorf("documents: no %s document", documentType)
	}
	return path(applicationDir(s.base, applicationID), documentType), nil
}

// Write replaces applicationID's documentType document with content,
// creating the application's directory if needed, and returns its path.
//
// The content goes to a temporary file in the same directory, which is
// then renamed over the document. Another process reading the document
// (an export, a review, documents.Current) sees the old version or the
// new one, never an empty or partial file, and a failed write leaves the
// old version as it was (RFC 0007, H3).
func (s *Store) Write(applicationID int64, documentType Type, content string) (string, error) {
	return s.write(applicationID, documentType, content, nil)
}

// ErrChanged is WriteIfUnchanged's error when the document on disk isn't
// the version the caller expected.
var ErrChanged = errors.New("documents: document changed since it was read")

// WriteIfUnchanged is Write, refused with ErrChanged unless the document
// on disk still has expectedSHA256 (see ContentSHA256). An empty
// expectedSHA256 expects no document, so a fresh draft can't replace one
// that appeared in the meantime (RFC 0007, H2). The check runs just before
// the rename; a write landing between the two still wins.
func (s *Store) WriteIfUnchanged(applicationID int64, documentType Type, content, expectedSHA256 string) (string, error) {
	return s.write(applicationID, documentType, content, func() error {
		got, err := s.SHA256(applicationID, documentType)
		if err != nil {
			return err
		}
		if got != expectedSHA256 {
			return ErrChanged
		}
		return nil
	})
}

// write is Write, with check (if any) run once the new content is ready
// to rename into place, so the window between them stays small.
func (s *Store) write(applicationID int64, documentType Type, content string, check func() error) (string, error) {
	p, err := s.Path(applicationID, documentType)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(p)+".tmp-*")
	if err != nil {
		return "", err
	}
	if err := writeAndClose(tmp, content); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if check != nil {
		if err := check(); err != nil {
			_ = os.Remove(tmp.Name())
			return "", err
		}
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return p, nil
}

// writeAndClose fills Write's temporary file. The mode is what
// os.WriteFile gave documents before (CreateTemp makes them 0600), and
// the sync makes sure the content is on disk before the rename can
// expose it.
func writeAndClose(f *os.File, content string) error {
	_, err := f.WriteString(content)
	if err == nil {
		err = f.Chmod(0o644)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// ContentSHA256 is the hex SHA-256 of a document's content: the version
// an agent read, which write_document can require to be unchanged.
func ContentSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// SHA256 is the ContentSHA256 of applicationID's documentType document
// as it is on disk now, or empty when it doesn't exist.
func (s *Store) SHA256(applicationID int64, documentType Type) (string, error) {
	p, err := s.Path(applicationID, documentType)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ContentSHA256(string(content)), nil
}

// canonicalDir holds the user-maintained files every application draws
// on (#199). It sits under the base directory so it stays as uncommitted
// as the drafts; "canonical" can't collide with an application directory,
// which is always numeric.
func canonicalDir(base string) string {
	return filepath.Join(base, "canonical")
}

// CanonicalResumePath is where the user's canonical resume lives, whether
// or not it exists: the maintained baseline an agent tailors per posting.
func (s *Store) CanonicalResumePath() string {
	return filepath.Join(canonicalDir(s.base), "resume.md")
}

// ProfilePath is where the user's profile reference lives, whether or not
// it exists: their background, experience and voice notes, the source an
// agent drafts from.
func (s *Store) ProfilePath() string {
	return filepath.Join(canonicalDir(s.base), "profile.md")
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
	return Status{docs: docs}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
