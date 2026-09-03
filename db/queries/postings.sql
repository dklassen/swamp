-- name: CreatePosting :one
INSERT INTO postings (
    company_id, source, source_id, title, department, team, location,
    employment_type, workplace_type, description_html, description_text,
    job_url, application_url, published_at, raw_payload
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdatePosting :one
-- Updates ingested content fields only. Deliberately does not touch
-- listing_status: transitions between open/closed are explicit domain
-- decisions (see MarkPostingClosed/MarkPostingReopened), not a side effect
-- of re-ingesting content. Does not touch company_id: a given (source,
-- source_id) is assumed to belong to the same company for its lifetime.
UPDATE postings
SET title = ?,
    department = ?,
    team = ?,
    location = ?,
    employment_type = ?,
    workplace_type = ?,
    description_html = ?,
    description_text = ?,
    job_url = ?,
    application_url = ?,
    published_at = ?,
    raw_payload = ?,
    last_seen_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: GetPosting :one
SELECT * FROM postings
WHERE id = ?;

-- name: GetPostingBySourceAndSourceID :one
SELECT * FROM postings
WHERE source = ? AND source_id = ?;

-- name: ListPostingsByCompany :many
-- id DESC is a tiebreaker: a single sync inserts many rows within the same
-- CURRENT_TIMESTAMP second (sqlite has only second resolution), so
-- first_seen_at alone leaves ties with no guaranteed order.
SELECT * FROM postings
WHERE company_id = ?
ORDER BY first_seen_at DESC, id DESC;

-- name: MarkPostingClosed :exec
UPDATE postings
SET listing_status = 'closed', updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: MarkPostingReopened :exec
UPDATE postings
SET listing_status = 'open', updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListDistinctDepartmentsForCompany :many
-- Keyspace discovery for filter selection: department is a company-
-- specific vocabulary (not a fixed enum), so filter values are offered
-- from what's actually been ingested, not guessed at. No "IS NOT NULL"
-- check needed -- department is NOT NULL DEFAULT '' (see decisions.log,
-- #74), so excluding '' is the only filter required.
SELECT DISTINCT department FROM postings
WHERE company_id = ? AND department != ''
ORDER BY department;

-- name: ListDistinctLocationsForCompany :many
SELECT DISTINCT location FROM postings
WHERE company_id = ? AND location != ''
ORDER BY location;

-- name: ListInterestedPostings :many
-- Postings the user has flagged interested and not archived, joined with
-- their company name and -- if one has been started -- their
-- application's id and status. Feeds the stage package's discovery of
-- work for the external-agent hand-off mechanism (see stage.List).
--
-- sqlc.embed(postings) for the always-present side (postings is inner-
-- joined via posting_markup, never nullable here) -- same reasoning as
-- ListActiveApplications (see decisions.log, ApplicationView). Deliberately
-- NOT sqlc.embed(applications): that side is LEFT JOINed (an application
-- may not exist yet) and sqlc.embed has a documented bug scanning a NULL
-- embedded struct on sqlite (sqlc-dev/sqlc#2997) -- kept as individually
-- aliased nullable columns, handled by the existing sql.NullInt64/
-- sql.NullString .Valid checks in interestedPostingFromRow.
SELECT
    sqlc.embed(postings),
    companies.name AS company_name,
    applications.id AS application_id,
    applications.status AS application_status
FROM postings
JOIN posting_markup ON posting_markup.posting_id = postings.id
JOIN companies ON companies.id = postings.company_id
LEFT JOIN applications ON applications.posting_id = postings.id
WHERE posting_markup.interested_at IS NOT NULL
  AND posting_markup.archived_at IS NULL
ORDER BY posting_markup.interested_at DESC;
