package migrations

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"
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

	sqlDB := migrateTo(t, Latest())
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

// TestPostingScores_ScoresKeepTheirRunsProvenance: 00029 moves what a run
// recorded onto each of its scores, and drops runs.
func TestPostingScores_ScoresKeepTheirRunsProvenance(t *testing.T) {
	t.Parallel()

	sqlDB := migrateTo(t, 28)
	insertPosting(t, sqlDB)
	for _, stmt := range []string{
		`INSERT INTO scoring_runs (model, client_name, client_version, profile_sha256, scope) VALUES ('claude-opus-5-5', 'claude-code', '2.4.1', 'profile', 'unscored')`,
		`INSERT INTO posting_scores (run_id, posting_id, score, reason, content_sha256) VALUES (1, 1, 4, 'Go, remote', 'content')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := goose.UpTo(sqlDB, ".", 29); err != nil {
		t.Fatalf("migrate to 29: %v", err)
	}

	var id, score int64
	var reason, content, profile, model, client, version string
	if err := sqlDB.QueryRow(`SELECT id, score, reason, content_sha256, profile_sha256, model, client_name, client_version FROM posting_scores`).
		Scan(&id, &score, &reason, &content, &profile, &model, &client, &version); err != nil {
		t.Fatalf("read score: %v", err)
	}
	if id != 1 || score != 4 || reason != "Go, remote" || content != "content" || profile != "profile" || model != "claude-opus-5-5" || client != "claude-code" || version != "2.4.1" {
		t.Errorf("score = %d %d %q %q %q %q %q %q, want score 1 with its run's model, client and profile", id, score, reason, content, profile, model, client, version)
	}
	var runs int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'scoring_runs'`).Scan(&runs); err != nil {
		t.Fatalf("look for scoring_runs: %v", err)
	}
	if runs != 0 {
		t.Error("scoring_runs is still there")
	}
}
