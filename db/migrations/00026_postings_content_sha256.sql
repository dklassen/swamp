-- +goose Up
-- +goose StatementBegin

-- The version of the content the agent reads (contenthash.Of), so a fit
-- score can be tied to what was scored (RFC 0009). '' until 00027 fills
-- in existing rows; IngestPosting sets it from then on.
ALTER TABLE postings ADD COLUMN content_sha256 TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE postings DROP COLUMN content_sha256;
-- +goose StatementEnd
