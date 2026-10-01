package documents

import (
	"fmt"
	"os"
)

// Current keeps the records (document reviews, exports) that still
// describe what's on disk: those whose document exists and whose content
// isCurrent says matches. A record of an earlier draft is dropped, the
// same as no record at all, since the version it describes no longer
// exists. isCurrent is store.DocumentReview.IsCurrent or
// store.DocumentExport.IsCurrent; taking it as a function keeps this
// package free of store.
//
// This is the only place documents reads content, and only to make this
// comparison. It replaced three copies, in stage, tui and cmd/swamp (RFC
// 0004). A read failure is an error rather than "changed": treating it
// as changed could hide a flagged review with no sign anything went
// wrong.
func Current[R any](status Status, records map[Type]R, isCurrent func(R, string) bool) (map[Type]R, error) {
	current := make(map[Type]R, len(records))
	for documentType, record := range records {
		doc, err := status.Doc(documentType)
		if err != nil {
			return nil, err
		}
		if !doc.Exists {
			continue
		}
		content, err := os.ReadFile(doc.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", doc.Path, err)
		}
		if isCurrent(record, string(content)) {
			current[documentType] = record
		}
	}
	return current, nil
}
