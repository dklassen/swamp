package document

import (
	"crypto/sha256"
	"encoding/hex"
)

// ContentSHA256 is the hex SHA-256 of a document's content: the version
// an agent read, which write_document can require to be unchanged.
func ContentSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
