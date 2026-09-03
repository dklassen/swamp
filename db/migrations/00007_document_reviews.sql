-- +goose Up
-- +goose StatementBegin

-- Document reviews: a human's pass over a drafted cover letter or resume,
-- owned by the user, append-only (a review is never edited or deleted
-- once created -- see #51's comment thread: "we don't overwrite
-- reviews"). content_snapshot captures the document's content as of the
-- moment it was reviewed, because documents.Store overwrites
-- cover_letter.md/resume.md in place with no versioning of its own -- a
-- review that only pointed at the file's current path would become
-- unreadable the moment the file gets redrafted. content_sha256 is a
-- derived, indexed shortcut for "did this change since the last review"
-- without comparing full TEXT blobs -- not queried by any code yet, but
-- indexed now since content_snapshot itself deliberately isn't (a full
-- TEXT index would be wasted weight for a column nothing does equality
-- lookups against).
--
-- cycle is the Nth review for this (application_id, document_type) pair,
-- computed by the store package at insert time (COUNT + 1), not supplied
-- by the caller -- see store.CreateDocumentReview.
--
-- document_type/outcome are plain TEXT with a CHECK constraint, not a Go
-- enum: unlike applications.status (see 00004_drop_application_status_
-- check_constraint.sql), this is a small, stable set with no history of
-- churn, so it follows interview_stages.outcome's pattern instead.
CREATE TABLE document_reviews (
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

CREATE INDEX idx_document_reviews_application_id ON document_reviews(application_id);
CREATE INDEX idx_document_reviews_content_sha256 ON document_reviews(content_sha256);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS document_reviews;

-- +goose StatementEnd
