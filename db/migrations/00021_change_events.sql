-- +goose Up
-- +goose StatementBegin

-- The change log (RFC 0008, #267): one row per change to a logged table,
-- written by triggers in the same transaction as the change, so the log
-- can't disagree with the data. Each Swamp process reads the events past
-- the last one it saw, to learn what other processes changed.
--
-- old/new hold only the logged columns, as JSON. origin says which process
-- made the change ("tui:4120"); these triggers leave it NULL, and each
-- Swamp connection stamps it with a TEMP trigger of its own (store.Open).
-- The triggers here call no Go function, so a writer outside Swamp (the
-- sqlite3 CLI, an older binary) still works, and its changes are logged
-- with origin NULL.
--
-- AUTOINCREMENT: ids are the readers' cursor, so one must never be reused.
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

-- applications: status, notes, deleted_at. updated_at isn't logged, so an
-- update that only touches it logs nothing.
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

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS applications_change_insert;
DROP TRIGGER IF EXISTS applications_change_update;
DROP TRIGGER IF EXISTS applications_change_delete;
DROP TABLE IF EXISTS change_events;

-- +goose StatementEnd
