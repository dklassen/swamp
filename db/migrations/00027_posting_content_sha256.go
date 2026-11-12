package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// Fills in content_sha256 for postings stored before 00026. In Go because
// SQLite has no SHA-256, and it must be the hash IngestPosting computes.
func init() {
	registerGoMigration("00027_posting_content_sha256.go", upPostingContentSHA256, downPostingContentSHA256)
}

func upPostingContentSHA256(ctx context.Context, tx *sql.Tx) error {
	hashes, err := postingHashes(ctx, tx)
	if err != nil {
		return err
	}
	for id, hash := range hashes {
		if _, err := tx.ExecContext(ctx, `UPDATE postings SET content_sha256 = ? WHERE id = ?`, hash, id); err != nil {
			return fmt.Errorf("posting %d: %w", id, err)
		}
	}
	return nil
}

// postingHashes reads every posting's hash, closing the read before the
// updates go through the same transaction.
func postingHashes(ctx context.Context, tx *sql.Tx) (map[int64]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, title, department, location, workplace_type, description_text FROM postings`)
	if err != nil {
		return nil, fmt.Errorf("read postings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	hashes := map[int64]string{}
	for rows.Next() {
		var id int64
		var title, department, location, workplaceType, descriptionText string
		if err := rows.Scan(&id, &title, &department, &location, &workplaceType, &descriptionText); err != nil {
			return nil, fmt.Errorf("read posting: %w", err)
		}
		hashes[id] = contentSHA256(title, department, location, workplaceType, descriptionText)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read postings: %w", err)
	}
	return hashes, nil
}

// contentSHA256 is the hex SHA-256 of the length-prefixed fields, as
// posting.ContentSHA256 computed it when this migration was written.
func contentSHA256(fields ...string) string {
	var content []byte
	for _, field := range fields {
		content = fmt.Appendf(content, "%d:%s", len(field), field)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func downPostingContentSHA256(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE postings SET content_sha256 = ''`)
	return err
}
