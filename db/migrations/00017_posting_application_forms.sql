-- +goose Up
-- +goose StatementBegin

-- What a posting's application form asks for (#168): whether each
-- document is required, optional or absent, and the custom questions.
-- Fetched from the board when the user commits to the posting
-- (stage_prepare), not during sync, and re-fetched once it's older than
-- a week. One row per posting, replaced on each fetch.
--
-- form is jobboard.ApplicationForm as JSON. It's only ever read and
-- written whole, never queried by field, so a JSON column is simpler
-- than a table per question (#168). fetched_at is
-- CURRENT_TIMESTAMP (UTC, per AGENTS.md).
CREATE TABLE posting_application_forms (
    posting_id  INTEGER PRIMARY KEY REFERENCES postings(id),
    form        TEXT NOT NULL,
    fetched_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS posting_application_forms;

-- +goose StatementEnd
