package migrations

import (
	"database/sql"
	"testing"
)

// insertScore adds a scoring run and one score in it for a new posting,
// returning the score's ID.
func insertScore(t *testing.T, sqlDB *sql.DB) int64 {
	t.Helper()
	for _, stmt := range []string{
		`INSERT INTO companies (name, source, source_ref) VALUES ('Acme', 'ashby', 'acme')`,
		`INSERT INTO postings (company_id, source, source_id, title, raw_payload) VALUES (1, 'ashby', '1', 'Engineer', '{}')`,
		`INSERT INTO scoring_runs (model, profile_sha256, scope) VALUES ('claude-opus-5-5', 'abc', 'unscored')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	res, err := sqlDB.Exec(`INSERT INTO posting_scores (run_id, posting_id, score, reason, content_sha256) VALUES (1, 1, 4, 'Go, remote', 'def')`)
	if err != nil {
		t.Fatalf("insert score: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

func TestPostingScores_InsertingAScoreLogsOneEvent(t *testing.T) {
	t.Parallel()

	sqlDB := migrateTo(t, Latest())
	id := insertScore(t, sqlDB)

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

func TestPostingScores_ASecondScoreForAPostingInOneRunIsRefused(t *testing.T) {
	t.Parallel()

	sqlDB := migrateTo(t, Latest())
	insertScore(t, sqlDB)

	if _, err := sqlDB.Exec(`INSERT INTO posting_scores (run_id, posting_id, score, reason, content_sha256) VALUES (1, 1, 2, 'again', 'def')`); err == nil {
		t.Error("a second score for posting 1 in run 1 was stored, want it refused")
	}
}
