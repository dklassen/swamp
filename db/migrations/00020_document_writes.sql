-- +goose Up
-- +goose StatementBegin

-- Document writes: one row per time Swamp saw a document written,
-- append-only. Inserting one commits, which moves PRAGMA data_version, so
-- a TUI polling store.ChangeProbe notices a draft an agent wrote (RFC
-- 0007, step 8). source is who wrote it: write_document (an agent through
-- MCP) or editor (the TUI's $EDITOR, recorded when it closes with the
-- file changed); edits made outside Swamp aren't recorded. content_sha256
-- is the version written, hashed like document_reviews.content_sha256.
-- document_type and source have no CHECK constraint, matching
-- document_reviews since 00008 (validated in Go). AUTOINCREMENT, so an
-- id is never reused.
CREATE TABLE document_writes (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    application_id  INTEGER NOT NULL REFERENCES applications(id),
    document_type   TEXT NOT NULL,
    content_sha256  TEXT NOT NULL,
    source          TEXT NOT NULL,
    written_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_document_writes_document ON document_writes(application_id, document_type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS document_writes;

-- +goose StatementEnd
