-- +goose Up
-- +goose StatementBegin

-- Close the open postings of already soft-deleted companies (#178), as
-- store.SoftDeleteCompany now does on delete: a deleted company is never
-- synced again, so nothing else would ever close them, and they counted
-- toward open totals. Applications are left alone, as on delete.
--
-- Each gets a "closed" posting_history row snapshotting it as it was,
-- like store.closePosting writes. The snapshot is built here with
-- json_object, using the same keys, in the same order, as store.Posting's
-- JSON. Times are RFC 3339 UTC to the second. Go would keep any
-- fractional seconds, which only published_at can have.
INSERT INTO posting_history (posting_id, change_type, snapshot)
SELECT p.id, 'closed', json_object(
    'ID', p.id,
    'CompanyID', p.company_id,
    'Source', p.source,
    'SourceID', p.source_id,
    'Title', p.title,
    'Department', p.department,
    'Team', p.team,
    'Location', p.location,
    'EmploymentType', p.employment_type,
    'WorkplaceType', p.workplace_type,
    'DescriptionHTML', p.description_html,
    'DescriptionText', p.description_text,
    'JobURL', p.job_url,
    'ApplicationURL', p.application_url,
    'PublishedAt', strftime('%Y-%m-%dT%H:%M:%SZ', p.published_at),
    'RawPayload', p.raw_payload,
    'ListingStatus', p.listing_status,
    'FirstSeenAt', strftime('%Y-%m-%dT%H:%M:%SZ', p.first_seen_at),
    'LastSeenAt', strftime('%Y-%m-%dT%H:%M:%SZ', p.last_seen_at),
    'CreatedAt', strftime('%Y-%m-%dT%H:%M:%SZ', p.created_at),
    'UpdatedAt', strftime('%Y-%m-%dT%H:%M:%SZ', p.updated_at)
)
FROM postings p
JOIN companies c ON c.id = p.company_id
WHERE c.deleted_at IS NOT NULL AND p.listing_status = 'open';

UPDATE postings
SET listing_status = 'closed', updated_at = CURRENT_TIMESTAMP
WHERE listing_status = 'open'
  AND company_id IN (SELECT id FROM companies WHERE deleted_at IS NOT NULL);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Nothing to undo. The postings the Up closed can't be told apart from
-- postings that sync closed before their company was deleted (both have
-- a "closed" history row), and leaving them open was the bug. Restoring
-- a company and syncing it reopens whatever is still listed.
SELECT 1;

-- +goose StatementEnd
