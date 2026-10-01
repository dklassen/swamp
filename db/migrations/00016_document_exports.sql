-- +goose Up
-- +goose StatementBegin

-- Document exports (#188): one row per PDF exported from a drafted
-- document, by the TUI export screen or `swamp export`, append-only.
-- content_sha256 is the hash of exactly the markdown that was rendered,
-- computed the same way as document_reviews.content_sha256, so an export
-- goes stale once the document changes (store.DocumentExport.IsCurrent).
-- path is where the PDF was written, for reference only; nothing checks
-- the file is still there. document_type has no CHECK constraint,
-- matching document_reviews since 00008 (validated in Go).
CREATE TABLE document_exports (
    id              INTEGER PRIMARY KEY,
    application_id  INTEGER NOT NULL REFERENCES applications(id),
    document_type   TEXT NOT NULL,
    content_sha256  TEXT NOT NULL,
    path            TEXT NOT NULL,
    exported_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_document_exports_application_id ON document_exports(application_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS document_exports;

-- +goose StatementEnd
