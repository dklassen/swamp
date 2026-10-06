-- +goose Up
-- +goose StatementBegin

-- The logged columns for each table are listed in changelog.go, which a
-- test checks these triggers against.

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

CREATE TRIGGER document_reviews_change_insert AFTER INSERT ON document_reviews
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('document_reviews', NEW.id, 'insert', json_object('application_id', NEW.application_id, 'document_type', NEW.document_type, 'outcome', NEW.outcome));
END;

CREATE TRIGGER document_exports_change_insert AFTER INSERT ON document_exports
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('document_exports', NEW.id, 'insert', json_object('application_id', NEW.application_id, 'document_type', NEW.document_type, 'content_sha256', NEW.content_sha256));
END;

CREATE TRIGGER document_writes_change_insert AFTER INSERT ON document_writes
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('document_writes', NEW.id, 'insert', json_object('application_id', NEW.application_id, 'document_type', NEW.document_type, 'source', NEW.source, 'content_sha256', NEW.content_sha256));
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS companies_change_insert;
DROP TRIGGER IF EXISTS companies_change_update;
DROP TRIGGER IF EXISTS companies_change_delete;
DROP TRIGGER IF EXISTS postings_change_insert;
DROP TRIGGER IF EXISTS postings_change_update;
DROP TRIGGER IF EXISTS postings_change_delete;
DROP TRIGGER IF EXISTS document_reviews_change_insert;
DROP TRIGGER IF EXISTS document_exports_change_insert;
DROP TRIGGER IF EXISTS document_writes_change_insert;

-- +goose StatementEnd
