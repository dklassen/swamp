-- +goose Up
-- +goose StatementBegin

-- One row per change to a logged table, written by triggers so the log
-- can't disagree with the data. The triggers call no Go function and
-- leave origin NULL: a writer outside Swamp (the sqlite3 CLI) still works.
-- Swamp's connections stamp origin themselves (store.Open).
--
-- AUTOINCREMENT: ids are readers' cursors, so one must never be reused.
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
