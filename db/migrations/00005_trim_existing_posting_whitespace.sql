-- +goose Up
-- +goose StatementBegin

-- Postings written before sync started trimming fetched fields (see
-- sync.sanitizePosting, added alongside this migration) can carry
-- padded whitespace on free-text columns -- e.g. Stripe's Greenhouse
-- listings showed up as both "Dublin" and "Dublin ". Untrimmed values
-- also broke filter.Match's exact comparison, so a clean "Canada"
-- company filter could silently fail to match a stored "Canada "
-- location. This backfills what's already in the table; new writes are
-- already covered by sync.sanitizePosting going forward.
--
-- raw_payload is deliberately left untouched: it's the source's raw
-- JSON response, kept verbatim for audit/debugging, not a display
-- field. published_at isn't a string.
UPDATE postings SET
    source_id         = TRIM(source_id),
    title             = TRIM(title),
    department        = TRIM(department),
    team              = TRIM(team),
    location          = TRIM(location),
    employment_type   = TRIM(employment_type),
    workplace_type    = TRIM(workplace_type),
    description_html  = TRIM(description_html),
    description_text  = TRIM(description_text),
    job_url           = TRIM(job_url),
    application_url   = TRIM(application_url);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- No-op: trimming discards the exact original whitespace, so there is
-- nothing to restore it from. Consistent with this project's "design for
-- undo, but 'can rollback' != 'can undo'" stance -- a fabricated
-- restoration (e.g. re-adding a single trailing space) would misrepresent
-- what was actually there before, which is worse than admitting this
-- step isn't reversible.
SELECT 1;

-- +goose StatementEnd
