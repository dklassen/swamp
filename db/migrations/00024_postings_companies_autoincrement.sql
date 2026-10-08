-- +goose Up
-- +goose StatementBegin

-- Change events, MCP clients and child rows hold company and posting IDs,
-- so one deleted by hand must never be handed to a new row. Rebuilt as in
-- 00019; new table first, so child foreign keys keep naming the table.
-- The change triggers are dropped first so the copy logs nothing.
DROP TRIGGER companies_change_insert;
DROP TRIGGER companies_change_update;
DROP TRIGGER companies_change_delete;
DROP TRIGGER postings_change_insert;
DROP TRIGGER postings_change_update;
DROP TRIGGER postings_change_delete;

CREATE TABLE companies_new (
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

INSERT INTO companies_new SELECT id, name, source, source_ref, deleted_at, created_at, updated_at, description, last_fetched_at, sync_lease_token, sync_lease_at FROM companies;
DROP TABLE companies;
ALTER TABLE companies_new RENAME TO companies;

CREATE TABLE postings_new (
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
    UNIQUE (source, source_id)
);

INSERT INTO postings_new SELECT id, company_id, source, source_id, title, department, team, location, employment_type, workplace_type, description_html, description_text, job_url, application_url, published_at, raw_payload, listing_status, first_seen_at, last_seen_at, created_at, updated_at FROM postings;
DROP TABLE postings;
ALTER TABLE postings_new RENAME TO postings;

CREATE INDEX idx_postings_company_id ON postings(company_id);
CREATE INDEX idx_postings_listing_status ON postings(listing_status);

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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER companies_change_insert;
DROP TRIGGER companies_change_update;
DROP TRIGGER companies_change_delete;
DROP TRIGGER postings_change_insert;
DROP TRIGGER postings_change_update;
DROP TRIGGER postings_change_delete;

CREATE TABLE companies_new (
    id               INTEGER PRIMARY KEY,
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

INSERT INTO companies_new SELECT id, name, source, source_ref, deleted_at, created_at, updated_at, description, last_fetched_at, sync_lease_token, sync_lease_at FROM companies;
DROP TABLE companies;
ALTER TABLE companies_new RENAME TO companies;

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

INSERT INTO postings_new SELECT id, company_id, source, source_id, title, department, team, location, employment_type, workplace_type, description_html, description_text, job_url, application_url, published_at, raw_payload, listing_status, first_seen_at, last_seen_at, created_at, updated_at FROM postings;
DROP TABLE postings;
ALTER TABLE postings_new RENAME TO postings;

CREATE INDEX idx_postings_company_id ON postings(company_id);
CREATE INDEX idx_postings_listing_status ON postings(listing_status);

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

-- +goose StatementEnd
