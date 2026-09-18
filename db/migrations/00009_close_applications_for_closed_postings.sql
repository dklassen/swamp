-- +goose Up
-- +goose StatementBegin

-- One-time backfill for issue #105. From this migration onward the
-- syncer closes an application when it sees a posting go open ->
-- closed (see sync.closeApplicationForClosedPosting), but that only
-- fires on the transition. Postings already sitting at
-- listing_status = 'closed' before the feature existed would never
-- produce that transition again, so their applications would stay in
-- the active list indefinitely. This catches them once.
--
-- The rule matches the syncer's exactly: only applications still at an
-- early status are closed. 'interviewing' and beyond are a live process
-- that a listing coming down does not end (companies routinely pull a
-- posting while still interviewing their existing pipeline), and the
-- already-terminal statuses are left as the record of how they actually
-- ended.
--
-- Status strings are written literally here rather than referencing Go's
-- store.ApplicationStatus: a migration is a historical record of what
-- ran against the DB at this point in time, and must not change meaning
-- later if the enum's Go-side names are refactored.
UPDATE applications
SET status = 'posting_closed',
    updated_at = CURRENT_TIMESTAMP
WHERE status IN ('application_started', 'application_submitted')
  AND posting_id IN (SELECT id FROM postings WHERE listing_status = 'closed');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Lossy on purpose, and the only honest option: the Up collapsed two
-- distinct statuses ('application_started' and 'application_submitted')
-- into one, and nothing records which row was which. Everything comes
-- back as 'application_started', the safer of the two to re-derive --
-- it understates progress rather than claiming an application was
-- submitted when it may never have been.
UPDATE applications
SET status = 'application_started',
    updated_at = CURRENT_TIMESTAMP
WHERE status = 'posting_closed';

-- +goose StatementEnd
