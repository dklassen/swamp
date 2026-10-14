-- +goose Up
-- +goose StatementBegin

-- Each score carries what produced it, instead of naming a run that did:
-- a run only defined what "rescore everything" meant, which pending
-- against a profile now does. Each score takes its run's provenance.
-- Rebuilt as in 00024, trigger first so the copy logs nothing.
DROP TRIGGER posting_scores_change_insert;

CREATE TABLE posting_scores_new (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    posting_id     INTEGER NOT NULL REFERENCES postings(id),
    score          INTEGER NOT NULL,
    reason         TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    profile_sha256 TEXT NOT NULL,
    model          TEXT NOT NULL,
    client_name    TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    scored_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO posting_scores_new (id, posting_id, score, reason, content_sha256, profile_sha256, model, client_name, client_version, scored_at)
SELECT s.id, s.posting_id, s.score, s.reason, s.content_sha256, r.profile_sha256, r.model, r.client_name, r.client_version, s.scored_at
FROM posting_scores s
JOIN scoring_runs r ON r.id = s.run_id;

DROP TABLE posting_scores;
ALTER TABLE posting_scores_new RENAME TO posting_scores;
DROP TABLE scoring_runs;

-- A posting's current score is its highest id.
CREATE INDEX idx_posting_scores_posting_id_id ON posting_scores(posting_id, id);

-- The logged columns are listed in changelog.go, which a test checks this
-- trigger against.
CREATE TRIGGER posting_scores_change_insert AFTER INSERT ON posting_scores
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('posting_scores', NEW.id, 'insert', json_object('posting_id', NEW.posting_id, 'score', NEW.score));
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- One run per distinct provenance. A run held one score per posting, so
-- of a posting's scores with the same provenance only the first is kept.
DROP TRIGGER posting_scores_change_insert;

CREATE TABLE scoring_runs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    model          TEXT NOT NULL,
    client_name    TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    profile_sha256 TEXT NOT NULL,
    scope          TEXT NOT NULL,
    started_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO scoring_runs (model, client_name, client_version, profile_sha256, scope)
SELECT DISTINCT model, client_name, client_version, profile_sha256, 'all'
FROM posting_scores;

CREATE TABLE posting_scores_old (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         INTEGER NOT NULL REFERENCES scoring_runs(id),
    posting_id     INTEGER NOT NULL REFERENCES postings(id),
    score          INTEGER NOT NULL,
    reason         TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    scored_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (run_id, posting_id)
);

INSERT OR IGNORE INTO posting_scores_old (id, run_id, posting_id, score, reason, content_sha256, scored_at)
SELECT s.id, r.id, s.posting_id, s.score, s.reason, s.content_sha256, s.scored_at
FROM posting_scores s
JOIN scoring_runs r ON r.model = s.model AND r.client_name = s.client_name AND r.client_version = s.client_version AND r.profile_sha256 = s.profile_sha256
ORDER BY s.id;

DROP TABLE posting_scores;
ALTER TABLE posting_scores_old RENAME TO posting_scores;

CREATE INDEX idx_posting_scores_posting_id_id ON posting_scores(posting_id, id);

CREATE TRIGGER posting_scores_change_insert AFTER INSERT ON posting_scores
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('posting_scores', NEW.id, 'insert', json_object('run_id', NEW.run_id, 'posting_id', NEW.posting_id, 'score', NEW.score));
END;

-- +goose StatementEnd
