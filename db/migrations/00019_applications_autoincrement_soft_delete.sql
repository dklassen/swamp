-- +goose Up
-- +goose StatementBegin

-- Without AUTOINCREMENT, SQLite gives a deleted maximum id to the next
-- insert, and that new application inherits whatever still refers to the
-- old id: its documents folder, or an agent holding the id from an earlier
-- stage_prepare. SQLite can't add AUTOINCREMENT with ALTER TABLE,
-- so rebuild, same pattern as 00004. Copying the ids seeds sqlite_sequence
-- with the current maximum.
--
-- Deleting an application is now a soft delete (deleted_at), keeping its
-- history, reviews and drafts. Starting again on the same posting creates
-- a fresh application, so posting_id is unique only among live rows: a
-- partial index replaces the column's UNIQUE.
CREATE TABLE applications_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    posting_id  INTEGER NOT NULL REFERENCES postings(id),
    status      TEXT,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMP
);

INSERT INTO applications_new (id, posting_id, status, notes, created_at, updated_at)
SELECT id, posting_id, status, notes, created_at, updated_at
FROM applications;

DROP TABLE applications;
ALTER TABLE applications_new RENAME TO applications;

CREATE UNIQUE INDEX applications_live_posting_id ON applications (posting_id)
WHERE deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- The old schema allows one application per posting, deleted or not, so
-- soft-deleted applications and the rows they own are dropped.
DELETE FROM application_status_history WHERE application_id IN (SELECT id FROM applications WHERE deleted_at IS NOT NULL);
DELETE FROM document_reviews WHERE application_id IN (SELECT id FROM applications WHERE deleted_at IS NOT NULL);
DELETE FROM document_exports WHERE application_id IN (SELECT id FROM applications WHERE deleted_at IS NOT NULL);
DELETE FROM interview_stages WHERE application_id IN (SELECT id FROM applications WHERE deleted_at IS NOT NULL);

CREATE TABLE applications_old (
    id          INTEGER PRIMARY KEY,
    posting_id  INTEGER NOT NULL UNIQUE REFERENCES postings(id),
    status      TEXT,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO applications_old (id, posting_id, status, notes, created_at, updated_at)
SELECT id, posting_id, status, notes, created_at, updated_at
FROM applications
WHERE deleted_at IS NULL;

DROP TABLE applications;
ALTER TABLE applications_old RENAME TO applications;

-- +goose StatementEnd
