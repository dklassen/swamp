-- +goose Up
-- +goose StatementBegin

-- A per-company sync lease (#150), so two syncs of one company -- from the
-- TUI, `swamp fetch` or a scheduled run, in the same or another process --
-- can't overlap. Overlapping syncs that fetched the board at different
-- moments could close a posting the other had just reopened, and end its
-- application with it. sync_lease_token identifies the holder, so only it
-- releases the lease; sync_lease_at (CURRENT_TIMESTAMP, UTC) lets a lease
-- left behind by a killed process expire. Both NULL means free.
ALTER TABLE companies ADD COLUMN sync_lease_token TEXT;
ALTER TABLE companies ADD COLUMN sync_lease_at TIMESTAMP;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE companies DROP COLUMN sync_lease_at;
ALTER TABLE companies DROP COLUMN sync_lease_token;
-- +goose StatementEnd
