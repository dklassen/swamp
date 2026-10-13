package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/dklassen/swamp/contenthash"
)

// Fills in content_sha256 for postings stored before 00026. In Go because
// SQLite has no SHA-256, and it must be the hash IngestPosting computes.
func init() {
	registerGoMigration("00027_posting_content_sha256.go", upPostingContentSHA256, downPostingContentSHA256)
}

func upPostingContentSHA256(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, title, department, location, workplace_type, description_text FROM postings`)
	if err != nil {
		return fmt.Errorf("read postings: %w", err)
	}
	hashes := map[int64]string{}
	for rows.Next() {
		var id int64
		var p contenthash.Posting
		if err := rows.Scan(&id, &p.Title, &p.Department, &p.Location, &p.WorkplaceType, &p.DescriptionText); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read posting: %w", err)
		}
		hashes[id] = contenthash.Of(p)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("read postings: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read postings: %w", err)
	}
	for id, hash := range hashes {
		if _, err := tx.ExecContext(ctx, `UPDATE postings SET content_sha256 = ? WHERE id = ?`, hash, id); err != nil {
			return fmt.Errorf("posting %d: %w", id, err)
		}
	}
	return nil
}

func downPostingContentSHA256(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE postings SET content_sha256 = ''`)
	return err
}
