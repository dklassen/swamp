-- +goose Up
-- +goose StatementBegin

-- postings' nine optional TEXT columns were nullable, mirroring nothing
-- distinguishing nil from empty at the Go layer -- #67 already dropped the
-- pointer types on store.Posting/CreatePostingParams for exactly this
-- reason. Leaving the DB layer nullable meant the possibility could leak
-- back in at any time: a new sqlc-generated field on a nullable column is
-- sql.NullString by default, so the next person to touch this area would
-- have to rediscover "we don't distinguish nil from empty here" as an
-- unenforced convention instead of the schema simply refusing the
-- ambiguity. NOT NULL DEFAULT '' closes that seam at the schema itself
-- (see decisions.log, #74). Confirmed via SQLite's own record format that
-- this trades nothing for space or performance: a NULL and an empty TEXT
-- both serialize to a zero-byte payload with a single-byte serial-type
-- varint, so there's no storage cost either way.
--
-- published_at (TIMESTAMP) deliberately stays nullable -- see #74's notes:
-- a NOT NULL timestamp needs a sentinel date (Go's zero value,
-- 0001-01-01), which is worse than nullable for anything querying this
-- table outside Go ("WHERE published_at IS NOT NULL" is honest SQL,
-- "WHERE published_at != '0001-01-01 00:00:00'" is a magic-value footgun).
--
-- SQLite has no ALTER TABLE ... ALTER COLUMN, so rebuild the table, same
-- pattern 00002/00003/00004 used. No PRAGMA foreign_keys dance: this
-- project has never turned foreign key enforcement on (no PRAGMA
-- foreign_keys=ON anywhere), and the pragma is a no-op mid-transaction
-- (which goose already wraps this migration in) regardless.
CREATE TABLE postings_new (
    id                  INTEGER PRIMARY KEY,
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
    UNIQUE (source, source_id)
);

INSERT INTO postings_new (
    id, company_id, source, source_id, title, department, team, location,
    employment_type, workplace_type, description_html, description_text,
    job_url, application_url, published_at, raw_payload,
    listing_status, first_seen_at, last_seen_at, created_at, updated_at
)
SELECT
    id, company_id, source, source_id, title,
    COALESCE(department, ''), COALESCE(team, ''), COALESCE(location, ''),
    COALESCE(employment_type, ''), COALESCE(workplace_type, ''),
    COALESCE(description_html, ''), COALESCE(description_text, ''),
    COALESCE(job_url, ''), COALESCE(application_url, ''),
    published_at, raw_payload,
    listing_status, first_seen_at, last_seen_at, created_at, updated_at
FROM postings;

DROP TABLE postings;
ALTER TABLE postings_new RENAME TO postings;

CREATE INDEX idx_postings_company_id ON postings(company_id);
CREATE INDEX idx_postings_listing_status ON postings(listing_status);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Reverts to nullable columns. No COALESCE needed going this direction --
-- empty string is (and always was) a valid value under the nullable
-- schema too, so existing '' values just stay '' rather than becoming
-- NULL; this is not a full undo of whichever rows were NULL before Up,
-- consistent with this project's "can rollback != can undo" stance.
CREATE TABLE postings_old (
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

INSERT INTO postings_old (
    id, company_id, source, source_id, title, department, team, location,
    employment_type, workplace_type, description_html, description_text,
    job_url, application_url, published_at, raw_payload,
    listing_status, first_seen_at, last_seen_at, created_at, updated_at
)
SELECT
    id, company_id, source, source_id, title, department, team, location,
    employment_type, workplace_type, description_html, description_text,
    job_url, application_url, published_at, raw_payload,
    listing_status, first_seen_at, last_seen_at, created_at, updated_at
FROM postings;

DROP TABLE postings;
ALTER TABLE postings_old RENAME TO postings;

CREATE INDEX idx_postings_company_id ON postings(company_id);
CREATE INDEX idx_postings_listing_status ON postings(listing_status);

-- +goose StatementEnd
