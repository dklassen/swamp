-- +goose Up
-- +goose StatementBegin

-- applications.status's CHECK (status IN (...)) constraint (introduced in
-- 00002) duplicated the same fixed set of valid values now maintained as
-- a Go enum (store.ApplicationStatus, see store/application_status.go and
-- decisions.log) -- two copies of the same list that could silently drift
-- out of sync, flagged in PR #16 review. Go is the sole source of truth
-- now: store.ParseApplicationStatus rejects anything outside the known
-- set when a row is read, so nothing is lost by dropping the DB-side
-- copy. SQLite has no ALTER TABLE ... DROP CONSTRAINT, so rebuild the
-- table without it, same pattern 00002/00003 used for posting_markup/
-- interview_stages.
--
-- status also drops its NOT NULL and DEFAULT 'application_started' here
-- (PR #17 review follow-up): the initial status is the application's
-- decision (store.CreateApplication now supplies it explicitly, see
-- db/queries/applications.sql), not something the schema should invent --
-- "DB is just a dumb bag of bytes." notes keeps its own NOT NULL DEFAULT
-- '' untouched; this is specifically about status.
CREATE TABLE applications_new (
    id          INTEGER PRIMARY KEY,
    posting_id  INTEGER NOT NULL UNIQUE REFERENCES postings(id),
    status      TEXT,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO applications_new (id, posting_id, status, notes, created_at, updated_at)
SELECT id, posting_id, status, notes, created_at, updated_at
FROM applications;

DROP TABLE applications;
ALTER TABLE applications_new RENAME TO applications;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restores the CHECK constraint applications had under 00002. If any row
-- was written with a status outside that set while the constraint was
-- dropped, this rebuild fails loudly (constraint violation on the copy)
-- rather than silently coercing it to something else -- consistent with
-- this project's "fail loudly, no silent fallback" convention.
CREATE TABLE applications_old (
    id          INTEGER PRIMARY KEY,
    posting_id  INTEGER NOT NULL UNIQUE REFERENCES postings(id),
    status      TEXT NOT NULL DEFAULT 'application_started'
                    CHECK (status IN (
                        'application_started',
                        'application_submitted',
                        'interviewing',
                        'rejected',
                        'offer_received',
                        'offer_accepted',
                        'offer_declined'
                    )),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO applications_old (id, posting_id, status, notes, created_at, updated_at)
SELECT id, posting_id, status, notes, created_at, updated_at
FROM applications;

DROP TABLE applications;
ALTER TABLE applications_old RENAME TO applications;

-- +goose StatementEnd
