-- +goose Up
-- +goose StatementBegin

-- Who made each application status change (#174): 'user' (the TUI, MCP),
-- 'sync' (a posting closing or reopening), or 'unknown'. A reopened
-- posting restores an application only from a posting_closed that sync
-- made, never one the user set. Rows written before this migration can't
-- say which, so they're 'unknown' and never restored. Go's
-- store.StatusChangedBy is the source of truth for the values, as with
-- application status itself (no CHECK constraint).
ALTER TABLE application_status_history ADD COLUMN changed_by TEXT NOT NULL DEFAULT 'unknown';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE application_status_history DROP COLUMN changed_by;
-- +goose StatementEnd
