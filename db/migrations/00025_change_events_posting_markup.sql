-- +goose Up
-- +goose StatementBegin

-- The logged columns are listed in changelog.go, which a test checks these
-- triggers against.

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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS posting_markup_change_insert;
DROP TRIGGER IF EXISTS posting_markup_change_update;
DROP TRIGGER IF EXISTS posting_markup_change_delete;

-- +goose StatementEnd
