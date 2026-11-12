package migrations

import (
	"database/sql"
	"testing"
)

// insertPosting adds a company and one posting, ID 1.
func insertPosting(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`INSERT INTO companies (name, source, source_ref) VALUES ('Acme', 'ashby', 'acme')`,
		`INSERT INTO postings (company_id, source, source_id, title, raw_payload) VALUES (1, 'ashby', '1', 'Engineer', '{}')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func TestPostingScores_InsertingAScoreLogsOneEvent(t *testing.T) {
	t.Parallel()

	sqlDB := migrated(t)
	insertPosting(t, sqlDB)
	res, err := sqlDB.Exec(`INSERT INTO posting_scores (posting_id, score, reason, content_sha256, profile_sha256, model)
		VALUES (1, 4, 'Go, remote', 'content', 'profile', 'claude-opus-5-5')`)
	if err != nil {
		t.Fatalf("insert score: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}

	var op string
	var rowID int64
	var events int
	if err := sqlDB.QueryRow(`SELECT count(*), COALESCE(max(op), ''), COALESCE(max(row_id), 0) FROM change_events WHERE table_name = 'posting_scores'`).Scan(&events, &op, &rowID); err != nil {
		t.Fatalf("read change_events: %v", err)
	}
	if events != 1 || op != "insert" || rowID != id {
		t.Errorf("posting_scores events: %d, op %q, row %d; want one insert naming score %d", events, op, rowID, id)
	}
}
