-- +goose Up
-- +goose StatementBegin

-- document_reviews.document_type/outcome's CHECK (... IN (...)) constraints
-- (introduced in 00007) duplicated the same fixed sets of valid values now
-- maintained as Go enums (store.DocumentType/store.ReviewOutcome, see
-- store/document_review.go and decisions.log) -- two copies of the same
-- lists that could silently drift out of sync, same problem
-- applications.status already had (see 00004). Go is the sole source of
-- truth now: store.ParseDocumentType/ParseReviewOutcome reject anything
-- outside the known set when a row is read, so nothing is lost by
-- dropping the DB-side copy. SQLite has no ALTER TABLE ... DROP
-- CONSTRAINT, so rebuild the table without it, same pattern 00004 used
-- for applications -- and, since this table (unlike applications) has its
-- own indexes, recreate those on the rebuilt table too.
CREATE TABLE document_reviews_new (
    id                INTEGER PRIMARY KEY,
    application_id    INTEGER NOT NULL REFERENCES applications(id),
    document_type     TEXT NOT NULL,
    cycle             INTEGER NOT NULL,
    content_snapshot  TEXT NOT NULL,
    content_sha256    TEXT NOT NULL,
    outcome           TEXT NOT NULL,
    notes             TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (application_id, document_type, cycle)
);

INSERT INTO document_reviews_new (id, application_id, document_type, cycle, content_snapshot, content_sha256, outcome, notes, created_at)
SELECT id, application_id, document_type, cycle, content_snapshot, content_sha256, outcome, notes, created_at
FROM document_reviews;

DROP TABLE document_reviews;
ALTER TABLE document_reviews_new RENAME TO document_reviews;

CREATE INDEX idx_document_reviews_application_id ON document_reviews(application_id);
CREATE INDEX idx_document_reviews_content_sha256 ON document_reviews(content_sha256);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restores the CHECK constraints document_reviews had under 00007. If any
-- row was written with a document_type/outcome outside those sets while
-- the constraints were dropped, this rebuild fails loudly (constraint
-- violation on the copy) rather than silently coercing it to something
-- else -- consistent with this project's "fail loudly, no silent
-- fallback" convention (see 00004's down for the same reasoning).
CREATE TABLE document_reviews_old (
    id                INTEGER PRIMARY KEY,
    application_id    INTEGER NOT NULL REFERENCES applications(id),
    document_type     TEXT NOT NULL CHECK (document_type IN ('cover_letter', 'resume')),
    cycle             INTEGER NOT NULL,
    content_snapshot  TEXT NOT NULL,
    content_sha256    TEXT NOT NULL,
    outcome           TEXT NOT NULL CHECK (outcome IN ('passed', 'flagged')),
    notes             TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (application_id, document_type, cycle)
);

INSERT INTO document_reviews_old (id, application_id, document_type, cycle, content_snapshot, content_sha256, outcome, notes, created_at)
SELECT id, application_id, document_type, cycle, content_snapshot, content_sha256, outcome, notes, created_at
FROM document_reviews;

DROP TABLE document_reviews;
ALTER TABLE document_reviews_old RENAME TO document_reviews;

CREATE INDEX idx_document_reviews_application_id ON document_reviews(application_id);
CREATE INDEX idx_document_reviews_content_sha256 ON document_reviews(content_sha256);

-- +goose StatementEnd
