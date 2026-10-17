package document

import (
	"fmt"
	"time"
)

// Source is who wrote a document, as far as Swamp saw.
type Source int

const (
	// SourceWriteDocument is an agent, through MCP's write_document.
	SourceWriteDocument Source = iota
	// SourceEditor is the user, through the TUI's $EDITOR.
	SourceEditor
)

// sourceNames is the DB string form of each source, the one place the
// mapping is defined (as outcomeNames).
var sourceNames = [...]string{
	SourceWriteDocument: "write_document",
	SourceEditor:        "editor",
}

// String is also the value stored in document_writes.source.
func (s Source) String() string {
	if s < 0 || int(s) >= len(sourceNames) {
		return fmt.Sprintf("Source(%d)", int(s))
	}
	return sourceNames[s]
}

// ParseSource fails loudly on a value it doesn't know, as ParseOutcome
// does.
func ParseSource(s string) (Source, error) {
	for i, name := range sourceNames {
		if name == s {
			return Source(i), nil
		}
	}
	return 0, fmt.Errorf("document: unknown document write source %q", s)
}

// Write is one write of a document that Swamp saw.
type Write struct {
	ID            int64
	ApplicationID int64
	DocumentType  Type
	ContentSHA256 string
	Source        Source
	WrittenAt     time.Time
}
