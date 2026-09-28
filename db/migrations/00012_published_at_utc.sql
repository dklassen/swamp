-- +goose Up
-- +goose StatementBegin

-- published_at was written by the sqlite driver as Go's time.Time.String()
-- ("2006-01-02 15:04:05.999999999 -0700 MST"), keeping whatever offset the
-- job board reported. store.Open now writes times in UTC as
-- "2006-01-02 15:04:05.999999999-07:00" (issue #140). This rewrites the
-- rows stored before that as the same instant in the new form, so every
-- value shares one format and offset, and comparing or sorting them as
-- text in SQL agrees with time order.
--
-- Written in SQL rather than as a Go migration so the goose CLI behind
-- `task migrate:*` can still run it. String() always puts the numeric
-- offset right after the time, so no zone name ("EDT", "UTC", or the
-- offset repeated) needs interpreting. With k the position of the space
-- after the time, counted from the start of the time:
--   substr(published_at, 1, 10 + k)  -> "2026-08-17 05:18:42.004"
--   substr(published_at, 12 + k, 3)  -> "-04"
--   substr(published_at, 15 + k, 2)  -> "00"
-- strftime converts "2026-08-17 05:18:42.004-04:00" to UTC whole seconds;
-- the fraction is copied across unchanged, keeping its full precision,
-- since an offset never changes it.
--
-- Only values in the old shape are touched: NULLs and anything already in
-- the new format are left as they are.
UPDATE postings
SET published_at =
    strftime('%Y-%m-%d %H:%M:%S',
        substr(published_at, 1, 10 + instr(substr(published_at, 12), ' '))
        || substr(published_at, 12 + instr(substr(published_at, 12), ' '), 3)
        || ':'
        || substr(published_at, 15 + instr(substr(published_at, 12), ' '), 2))
    || CASE
        WHEN instr(substr(published_at, 1, 10 + instr(substr(published_at, 12), ' ')), '.') > 0
        THEN substr(published_at, instr(published_at, '.'),
                    11 + instr(substr(published_at, 12), ' ') - instr(published_at, '.'))
        ELSE ''
       END
    || '+00:00'
WHERE published_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- No-op: each value's original offset is gone, so there's nothing to
-- restore it from. The converted values are still the same instants and
-- read back fine with or without this migration, so leaving them is
-- correct, not just the only option.
SELECT 1;

-- +goose StatementEnd
