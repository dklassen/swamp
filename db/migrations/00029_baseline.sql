-- +goose Up
-- +goose StatementBegin

-- Swamp's schema as of version 29, squashed from the migrations before it.
-- Value lists without a CHECK (statuses, document types, sources) are
-- validated in Go, so there's one copy of each list. REFERENCES documents
-- each link: foreign_keys is never turned on.

-- One row per change to a logged table, written by triggers so the log
-- can't disagree with the data. The triggers leave origin NULL, so a writer
-- outside Swamp still works; Swamp's connections stamp it (store.Open).
-- Which columns each table logs is in changelog.go, which a test checks the
-- triggers against. AUTOINCREMENT: ids are readers' cursors.
CREATE TABLE change_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    table_name  TEXT NOT NULL,
    row_id      INTEGER NOT NULL,
    op          TEXT NOT NULL,
    old         TEXT,
    new         TEXT,
    origin      TEXT,
    at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_change_events_table ON change_events(table_name, id);

-- source picks the board adapter; source_ref is what that adapter needs to
-- fetch. last_fetched_at is NULL until a fetch succeeds, and a failed fetch
-- leaves it alone. The sync lease keeps two syncs of one company from
-- overlapping: the token names the holder, the time lets a lease left by a
-- killed process expire, and both NULL means free. AUTOINCREMENT, like
-- postings: change events, MCP clients and child rows hold these ids, so
-- one deleted by hand must never be handed to a new row.
CREATE TABLE companies (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL,
    source           TEXT NOT NULL,
    source_ref       TEXT NOT NULL,
    deleted_at       TIMESTAMP,
    created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    description      TEXT NOT NULL DEFAULT '',
    last_fetched_at  TIMESTAMP,
    sync_lease_token TEXT,
    sync_lease_at    TIMESTAMP,
    UNIQUE (source, source_ref)
);

CREATE TRIGGER companies_change_insert AFTER INSERT ON companies
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('companies', NEW.id, 'insert', json_object('name', NEW.name, 'description', NEW.description, 'deleted_at', NEW.deleted_at));
END;

CREATE TRIGGER companies_change_update AFTER UPDATE ON companies
WHEN OLD.name IS NOT NEW.name OR OLD.description IS NOT NEW.description OR OLD.deleted_at IS NOT NEW.deleted_at
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old, new)
    VALUES ('companies', NEW.id, 'update', json_object('name', OLD.name, 'description', OLD.description, 'deleted_at', OLD.deleted_at),
            json_object('name', NEW.name, 'description', NEW.description, 'deleted_at', NEW.deleted_at));
END;

CREATE TRIGGER companies_change_delete AFTER DELETE ON companies
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old)
    VALUES ('companies', OLD.id, 'delete', json_object('name', OLD.name, 'description', OLD.description, 'deleted_at', OLD.deleted_at));
END;

-- Several rows for one field are OR'd; fields are AND'd. A field with no
-- rows doesn't constrain postings.
CREATE TABLE company_filters (
    id          INTEGER PRIMARY KEY,
    company_id  INTEGER NOT NULL REFERENCES companies(id),
    field       TEXT NOT NULL CHECK (field IN ('department', 'location')),
    value       TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_company_filters_company_id ON company_filters(company_id);

-- Ingested data and sync bookkeeping. raw_payload keeps the board's whole
-- response, so a field not yet promoted to a column is never lost. Optional
-- text is '' rather than NULL; published_at stays nullable rather than take
-- a sentinel date. content_sha256 is posting.ContentSHA256, so a fit score
-- is tied to the content it scored.
CREATE TABLE postings (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id          INTEGER NOT NULL REFERENCES companies(id),
    source              TEXT NOT NULL,
    source_id           TEXT NOT NULL,
    title               TEXT NOT NULL,
    department          TEXT NOT NULL DEFAULT '',
    team                TEXT NOT NULL DEFAULT '',
    location            TEXT NOT NULL DEFAULT '',
    employment_type     TEXT NOT NULL DEFAULT '',
    workplace_type      TEXT NOT NULL DEFAULT '',
    description_html    TEXT NOT NULL DEFAULT '',
    description_text    TEXT NOT NULL DEFAULT '',
    job_url             TEXT NOT NULL DEFAULT '',
    application_url     TEXT NOT NULL DEFAULT '',
    published_at        TIMESTAMP,
    raw_payload         TEXT NOT NULL,
    listing_status      TEXT NOT NULL DEFAULT 'open'
                            CHECK (listing_status IN ('open', 'closed')),
    first_seen_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    content_sha256      TEXT NOT NULL DEFAULT '',
    UNIQUE (source, source_id)
);

CREATE INDEX idx_postings_company_id ON postings(company_id);
CREATE INDEX idx_postings_listing_status ON postings(listing_status);

CREATE TRIGGER postings_change_insert AFTER INSERT ON postings
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('postings', NEW.id, 'insert', json_object('company_id', NEW.company_id, 'listing_status', NEW.listing_status, 'title', NEW.title, 'department', NEW.department, 'team', NEW.team, 'location', NEW.location, 'workplace_type', NEW.workplace_type, 'employment_type', NEW.employment_type, 'published_at', NEW.published_at, 'updated_at', NEW.updated_at));
END;

CREATE TRIGGER postings_change_update AFTER UPDATE ON postings
WHEN OLD.company_id IS NOT NEW.company_id OR OLD.listing_status IS NOT NEW.listing_status OR OLD.title IS NOT NEW.title OR OLD.department IS NOT NEW.department OR OLD.team IS NOT NEW.team OR OLD.location IS NOT NEW.location OR OLD.workplace_type IS NOT NEW.workplace_type OR OLD.employment_type IS NOT NEW.employment_type OR OLD.published_at IS NOT NEW.published_at OR OLD.updated_at IS NOT NEW.updated_at
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old, new)
    VALUES ('postings', NEW.id, 'update', json_object('company_id', OLD.company_id, 'listing_status', OLD.listing_status, 'title', OLD.title, 'department', OLD.department, 'team', OLD.team, 'location', OLD.location, 'workplace_type', OLD.workplace_type, 'employment_type', OLD.employment_type, 'published_at', OLD.published_at, 'updated_at', OLD.updated_at),
            json_object('company_id', NEW.company_id, 'listing_status', NEW.listing_status, 'title', NEW.title, 'department', NEW.department, 'team', NEW.team, 'location', NEW.location, 'workplace_type', NEW.workplace_type, 'employment_type', NEW.employment_type, 'published_at', NEW.published_at, 'updated_at', NEW.updated_at));
END;

CREATE TRIGGER postings_change_delete AFTER DELETE ON postings
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old)
    VALUES ('postings', OLD.id, 'delete', json_object('company_id', OLD.company_id, 'listing_status', OLD.listing_status, 'title', OLD.title, 'department', OLD.department, 'team', OLD.team, 'location', OLD.location, 'workplace_type', OLD.workplace_type, 'employment_type', OLD.employment_type, 'published_at', OLD.published_at, 'updated_at', OLD.updated_at));
END;

-- A snapshot of a posting's previous ingested state, written when a fetch
-- finds its content changed or its listing closed or reopened.
CREATE TABLE posting_history (
    id                  INTEGER PRIMARY KEY,
    posting_id          INTEGER NOT NULL REFERENCES postings(id),
    change_type         TEXT NOT NULL
                            CHECK (change_type IN ('content_updated', 'closed', 'reopened')),
    snapshot            TEXT NOT NULL,
    recorded_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_posting_history_posting_id ON posting_history(posting_id);

-- Soft-deleted, so a deleted tag's posting_tags rows survive.
CREATE TABLE tags (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    deleted_at  TIMESTAMP,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE posting_tags (
    posting_id  INTEGER NOT NULL REFERENCES postings(id),
    tag_id      INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (posting_id, tag_id)
);

-- The user's triage of a posting, one row each. Interested and archived
-- are independent: a posting can be archived after being interesting.
CREATE TABLE posting_markup (
    posting_id    INTEGER PRIMARY KEY REFERENCES postings(id),
    interested_at TIMESTAMP,
    archived_at   TIMESTAMP,
    notes         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER posting_markup_change_insert AFTER INSERT ON posting_markup
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('posting_markup', NEW.posting_id, 'insert', json_object('interested_at', NEW.interested_at, 'archived_at', NEW.archived_at));
END;

CREATE TRIGGER posting_markup_change_update AFTER UPDATE ON posting_markup
WHEN OLD.interested_at IS NOT NEW.interested_at OR OLD.archived_at IS NOT NEW.archived_at
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old, new)
    VALUES ('posting_markup', NEW.posting_id, 'update',
            json_object('interested_at', OLD.interested_at, 'archived_at', OLD.archived_at),
            json_object('interested_at', NEW.interested_at, 'archived_at', NEW.archived_at));
END;

CREATE TRIGGER posting_markup_change_delete AFTER DELETE ON posting_markup
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old)
    VALUES ('posting_markup', OLD.posting_id, 'delete', json_object('interested_at', OLD.interested_at, 'archived_at', OLD.archived_at));
END;

-- What a posting's application form asks for, as JSON read and written
-- whole: one row per posting, replaced on each fetch.
CREATE TABLE posting_application_forms (
    posting_id  INTEGER PRIMARY KEY REFERENCES postings(id),
    form        TEXT NOT NULL,
    fetched_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Insert-only: a posting's current score is its highest id. Each score
-- carries what produced it.
CREATE TABLE posting_scores (
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

CREATE INDEX idx_posting_scores_posting_id_id ON posting_scores(posting_id, id);

CREATE TRIGGER posting_scores_change_insert AFTER INSERT ON posting_scores
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('posting_scores', NEW.id, 'insert', json_object('posting_id', NEW.posting_id, 'score', NEW.score));
END;

-- The user's pursuit of a posting, separate from the posting's own
-- lifecycle. Deleting one is a soft delete that keeps its history, reviews
-- and drafts, so posting_id is unique only among live rows. AUTOINCREMENT:
-- a reused id would inherit the old application's documents folder and any
-- agent still holding it. status has no default: the caller decides it.
CREATE TABLE applications (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    posting_id  INTEGER NOT NULL REFERENCES postings(id),
    status      TEXT,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMP
);

CREATE UNIQUE INDEX applications_live_posting_id ON applications (posting_id)
WHERE deleted_at IS NULL;

-- An application's events name its posting, so a screen showing a posting
-- with no application yet can tell one was started on it.
CREATE TRIGGER applications_change_insert AFTER INSERT ON applications
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('applications', NEW.id, 'insert',
            json_object('posting_id', NEW.posting_id, 'status', NEW.status, 'notes', NEW.notes, 'deleted_at', NEW.deleted_at));
END;

CREATE TRIGGER applications_change_update AFTER UPDATE ON applications
WHEN OLD.posting_id IS NOT NEW.posting_id OR OLD.status IS NOT NEW.status OR OLD.notes IS NOT NEW.notes OR OLD.deleted_at IS NOT NEW.deleted_at
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old, new)
    VALUES ('applications', NEW.id, 'update',
            json_object('posting_id', OLD.posting_id, 'status', OLD.status, 'notes', OLD.notes, 'deleted_at', OLD.deleted_at),
            json_object('posting_id', NEW.posting_id, 'status', NEW.status, 'notes', NEW.notes, 'deleted_at', NEW.deleted_at));
END;

CREATE TRIGGER applications_change_delete AFTER DELETE ON applications
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old)
    VALUES ('applications', OLD.id, 'delete',
            json_object('posting_id', OLD.posting_id, 'status', OLD.status, 'notes', OLD.notes, 'deleted_at', OLD.deleted_at));
END;

CREATE TABLE interview_stages (
    id              INTEGER PRIMARY KEY,
    application_id  INTEGER NOT NULL REFERENCES applications(id),
    sequence        INTEGER NOT NULL,
    name            TEXT NOT NULL,
    stage_date      DATE,
    outcome         TEXT NOT NULL DEFAULT 'pending'
                        CHECK (outcome IN ('pending', 'passed', 'failed')),
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_interview_stages_application_id ON interview_stages(application_id);

-- One row per status an application enters, append-only, written with the
-- change itself. changed_by is 'unknown' for rows from before it was
-- recorded; only a closing that sync made is ever undone by a reopening.
CREATE TABLE application_status_history (
    id              INTEGER PRIMARY KEY,
    application_id  INTEGER NOT NULL REFERENCES applications(id),
    status          TEXT NOT NULL,
    changed_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    changed_by      TEXT NOT NULL DEFAULT 'unknown'
);

CREATE INDEX idx_application_status_history_application_id ON application_status_history(application_id);

-- A review of a drafted document, append-only. content_snapshot keeps what
-- was reviewed, since documents are overwritten in place. cycle is the Nth
-- review of that application's document.
CREATE TABLE document_reviews (
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

CREATE INDEX idx_document_reviews_application_id ON document_reviews(application_id);
CREATE INDEX idx_document_reviews_content_sha256 ON document_reviews(content_sha256);

CREATE TRIGGER document_reviews_change_insert AFTER INSERT ON document_reviews
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('document_reviews', NEW.id, 'insert', json_object('application_id', NEW.application_id, 'document_type', NEW.document_type, 'outcome', NEW.outcome));
END;

-- One row per PDF exported, append-only. content_sha256 is the markdown
-- rendered, so an export goes stale once its document changes. path is for
-- reference only: nothing checks the file is still there.
CREATE TABLE document_exports (
    id              INTEGER PRIMARY KEY,
    application_id  INTEGER NOT NULL REFERENCES applications(id),
    document_type   TEXT NOT NULL,
    content_sha256  TEXT NOT NULL,
    path            TEXT NOT NULL,
    exported_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_document_exports_application_id ON document_exports(application_id);

CREATE TRIGGER document_exports_change_insert AFTER INSERT ON document_exports
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('document_exports', NEW.id, 'insert', json_object('application_id', NEW.application_id, 'document_type', NEW.document_type, 'content_sha256', NEW.content_sha256));
END;

-- One row per document write Swamp saw, append-only: by an agent through
-- MCP or the TUI's editor. Edits made outside Swamp aren't recorded.
CREATE TABLE document_writes (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    application_id  INTEGER NOT NULL REFERENCES applications(id),
    document_type   TEXT NOT NULL,
    content_sha256  TEXT NOT NULL,
    source          TEXT NOT NULL,
    written_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_document_writes_document ON document_writes(application_id, document_type);

CREATE TRIGGER document_writes_change_insert AFTER INSERT ON document_writes
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('document_writes', NEW.id, 'insert', json_object('application_id', NEW.application_id, 'document_type', NEW.document_type, 'source', NEW.source, 'content_sha256', NEW.content_sha256));
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE document_writes;
DROP TABLE document_exports;
DROP TABLE document_reviews;
DROP TABLE application_status_history;
DROP TABLE interview_stages;
DROP TABLE applications;
DROP TABLE posting_scores;
DROP TABLE posting_application_forms;
DROP TABLE posting_markup;
DROP TABLE posting_tags;
DROP TABLE tags;
DROP TABLE posting_history;
DROP TABLE postings;
DROP TABLE company_filters;
DROP TABLE companies;
DROP TABLE change_events;
-- +goose StatementEnd
