// Package contenthash identifies the version of a posting's content the
// agent reads, so a fit score can be tied to what it scored (RFC 0009).
// Both store, as it ingests a posting, and the migration that backfills
// existing ones compute it here, so the two can't disagree.
package contenthash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Posting is the content of a posting the agent reads. A field it doesn't
// read, such as the raw payload or the job URL, is left out, so a change
// to one doesn't ask for a rescore.
type Posting struct {
	Title, Department, Location, WorkplaceType, DescriptionText string
}

// Of is the hex SHA-256 of p. Each field is prefixed with its length, so
// moving text from one field to the next changes the hash.
func Of(p Posting) string {
	var content []byte
	for _, field := range []string{p.Title, p.Department, p.Location, p.WorkplaceType, p.DescriptionText} {
		content = fmt.Appendf(content, "%d:%s", len(field), field)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
