-- +goose Up
-- +goose StatementBegin

-- A short description of who the company is (what it builds, stage,
-- domain), so a company in the list means something without opening its
-- website. Written by agents adding companies they discovered (the MCP
-- add_company tool) and readable in the TUI. NOT NULL DEFAULT '' like
-- postings' optional TEXT columns (#74): nothing distinguishes "never
-- described" from "described as empty".
ALTER TABLE companies ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE companies DROP COLUMN description;
-- +goose StatementEnd
