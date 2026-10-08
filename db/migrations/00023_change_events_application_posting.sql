-- +goose Up
-- +goose StatementBegin

-- An application's events name its posting, so a screen showing a posting
-- with no application yet can tell one was started on it.
DROP TRIGGER applications_change_insert;
DROP TRIGGER applications_change_update;
DROP TRIGGER applications_change_delete;

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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER applications_change_insert;
DROP TRIGGER applications_change_update;
DROP TRIGGER applications_change_delete;

CREATE TRIGGER applications_change_insert AFTER INSERT ON applications
BEGIN
    INSERT INTO change_events (table_name, row_id, op, new)
    VALUES ('applications', NEW.id, 'insert',
            json_object('status', NEW.status, 'notes', NEW.notes, 'deleted_at', NEW.deleted_at));
END;

CREATE TRIGGER applications_change_update AFTER UPDATE ON applications
WHEN OLD.status IS NOT NEW.status OR OLD.notes IS NOT NEW.notes OR OLD.deleted_at IS NOT NEW.deleted_at
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old, new)
    VALUES ('applications', NEW.id, 'update',
            json_object('status', OLD.status, 'notes', OLD.notes, 'deleted_at', OLD.deleted_at),
            json_object('status', NEW.status, 'notes', NEW.notes, 'deleted_at', NEW.deleted_at));
END;

CREATE TRIGGER applications_change_delete AFTER DELETE ON applications
BEGIN
    INSERT INTO change_events (table_name, row_id, op, old)
    VALUES ('applications', OLD.id, 'delete',
            json_object('status', OLD.status, 'notes', OLD.notes, 'deleted_at', OLD.deleted_at));
END;

-- +goose StatementEnd
