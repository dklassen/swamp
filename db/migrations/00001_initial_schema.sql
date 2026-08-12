-- +goose Up
-- +goose StatementBegin

-- Companies: source-agnostic. `source` identifies which board adapter to
-- use ('ashby' today); `source_ref` is whatever that adapter needs to
-- fetch (Ashby's job-board slug, for now).
CREATE TABLE companies (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    source      TEXT NOT NULL,
    source_ref  TEXT NOT NULL,
    deleted_at  TIMESTAMP,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (source, source_ref)
);

-- Filters: owned. Multiple rows per (company, field) = OR within that
-- field. A posting must match at least one value in every field that has
-- rows configured for that company (AND across fields). No rows for a
-- field = no constraint on that field.
CREATE TABLE company_filters (
    id          INTEGER PRIMARY KEY,
    company_id  INTEGER NOT NULL REFERENCES companies(id),
    field       TEXT NOT NULL CHECK (field IN ('department', 'location')),
    value       TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_company_filters_company_id ON company_filters(company_id);

-- Postings: ingested data + sync bookkeeping only. Canonical fields are
-- normalized across boards; `raw_payload` preserves the full source
-- response for anything not normalized, so a new board adapter never
-- loses data even before we decide to promote a field to canonical.
CREATE TABLE postings (
    id                  INTEGER PRIMARY KEY,
    company_id          INTEGER NOT NULL REFERENCES companies(id),
    source              TEXT NOT NULL,
    source_id           TEXT NOT NULL,
    title               TEXT NOT NULL,
    department          TEXT,
    team                TEXT,
    location            TEXT,
    employment_type     TEXT,
    workplace_type      TEXT,
    description_html    TEXT,
    description_text    TEXT,
    job_url             TEXT,
    application_url     TEXT,
    published_at        TIMESTAMP,
    raw_payload         TEXT NOT NULL,

    listing_status      TEXT NOT NULL DEFAULT 'open'
                            CHECK (listing_status IN ('open', 'closed')),
    first_seen_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (source, source_id)
);

CREATE INDEX idx_postings_company_id ON postings(company_id);
CREATE INDEX idx_postings_listing_status ON postings(listing_status);

-- Posting history: ingested-side. Full snapshot of the prior ingested
-- state, written whenever a re-fetch detects a diff in any ingested
-- field, or a listing_status transition.
CREATE TABLE posting_history (
    id                  INTEGER PRIMARY KEY,
    posting_id          INTEGER NOT NULL REFERENCES postings(id),
    change_type         TEXT NOT NULL
                            CHECK (change_type IN ('content_updated', 'closed', 'reopened')),
    snapshot            TEXT NOT NULL,
    recorded_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_posting_history_posting_id ON posting_history(posting_id);

-- Posting markup: owned, one row per posting. Single mutable status +
-- single mutable notes field (overwritten in place, not a log).
CREATE TABLE posting_markup (
    posting_id  INTEGER PRIMARY KEY REFERENCES postings(id),
    user_status TEXT NOT NULL DEFAULT 'new'
                    CHECK (user_status IN
                        ('new', 'interested', 'applied', 'interviewing', 'rejected', 'archived')),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Tags: owned, reusable across postings. Soft-deleted via deleted_at so
-- historical posting_tags associations survive a tag "deletion" instead
-- of being cascade-removed.
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

-- Interview stages: owned, ordered list per posting.
CREATE TABLE interview_stages (
    id          INTEGER PRIMARY KEY,
    posting_id  INTEGER NOT NULL REFERENCES postings(id),
    sequence    INTEGER NOT NULL,
    name        TEXT NOT NULL,
    stage_date  DATE,
    outcome     TEXT NOT NULL DEFAULT 'pending'
                    CHECK (outcome IN ('pending', 'passed', 'failed')),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_interview_stages_posting_id ON interview_stages(posting_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS interview_stages;
DROP TABLE IF EXISTS posting_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS posting_markup;
DROP TABLE IF EXISTS posting_history;
DROP TABLE IF EXISTS postings;
DROP TABLE IF EXISTS company_filters;
DROP TABLE IF EXISTS companies;

-- +goose StatementEnd
