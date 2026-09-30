-- +goose Up
-- +goose StatementBegin

-- Application status history (#162, RFC 0002 phase 0): one row per status
-- an application enters, append-only, written in the same transaction as
-- the status change itself (see store.recordApplicationStatus). Without it
-- every change overwrote the last, so e.g. a submitted application that
-- sync later moved to posting_closed was indistinguishable from one that
-- was never submitted. changed_at is CURRENT_TIMESTAMP (UTC, per
-- AGENTS.md).
CREATE TABLE application_status_history (
    id              INTEGER PRIMARY KEY,
    application_id  INTEGER NOT NULL REFERENCES applications(id),
    status          TEXT NOT NULL,
    changed_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_application_status_history_application_id ON application_status_history(application_id);

-- Backfill what can be known about existing applications: every
-- application started at created_at, and entered its current status (if
-- that's anything else) at updated_at. Any transitions in between were
-- never recorded and can't be recovered. updated_at also moves on a notes
-- edit, so the second row's time is an upper bound, not exact.
INSERT INTO application_status_history (application_id, status, changed_at)
SELECT id, 'application_started', created_at FROM applications;

INSERT INTO application_status_history (application_id, status, changed_at)
SELECT id, status, updated_at FROM applications
WHERE status IS NOT NULL AND status != 'application_started';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS application_status_history;

-- +goose StatementEnd
