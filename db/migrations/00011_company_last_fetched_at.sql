-- +goose Up
-- +goose StatementBegin

-- When this company's postings were last fetched successfully, for the
-- company list's "last fetched" column. Set by sync.SyncCompany after a
-- successful fetch; a failed fetch leaves it alone, so a stale value is a
-- visible sign something's wrong. Nullable rather than NOT NULL with a
-- sentinel, per #74's reasoning for timestamps: NULL reads as "never
-- fetched", which is true for every existing row until its next sync.
ALTER TABLE companies ADD COLUMN last_fetched_at TIMESTAMP;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE companies DROP COLUMN last_fetched_at;
-- +goose StatementEnd
