-- +goose Up
-- +goose StatementBegin

-- Fit scores. A run is one scoring session the agent
-- starts; a score is one posting's score within a run. Rows are only ever
-- inserted: a posting's current score is its newest. No CHECK on scope or
-- score: Go validates both, as 00004 and 00008 decided for value lists.
-- REFERENCES documents the links; foreign_keys isn't on (00006).
CREATE TABLE scoring_runs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    model          TEXT NOT NULL,
    client_name    TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    profile_sha256 TEXT NOT NULL,
    scope          TEXT NOT NULL,
    started_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE posting_scores (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         INTEGER NOT NULL REFERENCES scoring_runs(id),
    posting_id     INTEGER NOT NULL REFERENCES postings(id),
    score          INTEGER NOT NULL,
    reason         TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    scored_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (run_id, posting_id)
);

-- A posting's current score is its highest id.
CREATE INDEX idx_posting_scores_posting_id_id ON posting_scores(posting_id, id);

-- The logged columns are listed in changelog.go, which a test checks this
-- trigger against.
CREATE TRIGGER posting_scores_change_insert AFTER INSERT ON posting_scores
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('posting_scores', NEW.id, 'insert', json_object('run_id', NEW.run_id, 'posting_id', NEW.posting_id, 'score', NEW.score));
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE posting_scores;
DROP TABLE scoring_runs;
-- +goose StatementEnd
