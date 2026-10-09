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

-- name: ClosePostingIfOpen :execrows
-- Conditional, unlike MarkPostingClosed: 0 rows affected means the
-- posting was already closed (e.g. by an overlapping sync), so the
-- caller records nothing (see store.ClosePosting, #147).
UPDATE postings
SET listing_status = 'closed', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND listing_status = 'open';

-- name: ReopenPostingIfClosed :execrows
-- Conditional, unlike MarkPostingReopened: 0 rows affected means the
-- posting was already open again (e.g. reopened by an overlapping sync),
-- so the caller records nothing (see store.ReopenPosting, #148).
UPDATE postings
SET listing_status = 'open', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND listing_status = 'closed';

-- name: MarkPostingReopened :exec
UPDATE postings
SET listing_status = 'open', updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListDistinctDepartmentsForCompany :many
-- Keyspace discovery for filter selection: department is a company-
-- specific vocabulary (not a fixed enum), so filter values are offered
-- from what's actually been ingested, not guessed at. No "IS NOT NULL"
-- check needed -- department is NOT NULL DEFAULT '' (#74),
-- so excluding '' is the only filter required.
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
-- ListActiveApplications (see ApplicationView). Deliberately
-- NOT sqlc.embed(applications): that side is LEFT JOINed (an application
-- may not exist yet) and sqlc.embed has a documented bug scanning a NULL
-- embedded struct on sqlite (sqlc-dev/sqlc#2997) -- kept as individually
-- aliased nullable columns, handled by the existing sql.NullInt64/
-- sql.NullString .Valid checks in interestedPostingFromRow.
--
-- Postings whose application is at a terminal status are excluded: a
-- dead-end application isn't drafting work (#121). Same sqlc.slice
-- approach as ListActiveApplications, so store.TerminalApplicationStatuses
-- stays the sole source of truth for "terminal" -- including its caveat
-- that sqlc.slice can't safely combine with other bound parameters on
-- sqlite; this query has none. The applications.id IS NULL branch keeps
-- postings with no application yet, which the LEFT JOIN yields as NULLs.
SELECT
    sqlc.embed(postings),
    companies.name AS company_name,
    applications.id AS application_id,
    applications.status AS application_status
FROM postings
JOIN posting_markup ON posting_markup.posting_id = postings.id
JOIN companies ON companies.id = postings.company_id
LEFT JOIN applications ON applications.posting_id = postings.id AND applications.deleted_at IS NULL
WHERE posting_markup.interested_at IS NOT NULL
  AND posting_markup.archived_at IS NULL
  AND (applications.id IS NULL OR applications.status NOT IN (sqlc.slice('terminal_statuses')))
ORDER BY posting_markup.interested_at DESC;

-- name: CountOpenPostingsByCompany :many
-- Per-company count of what a company's posting list shows by default:
-- listings still open on the job board that the user hasn't archived.
-- Feeds the company list's "Open" column. Doesn't apply company_filters:
-- postings are already gated by filters at ingestion, and a filter added
-- later only narrows the posting list's display, not this count.
SELECT postings.company_id, COUNT(*) AS open_postings
FROM postings
JOIN posting_markup ON posting_markup.posting_id = postings.id
WHERE postings.listing_status = 'open'
  AND posting_markup.archived_at IS NULL
GROUP BY postings.company_id;

-- name: ListOpenPostingIDsByCompany :many
-- What store.SoftDeleteCompany closes: a deleted company is never synced
-- again, so nothing else would ever close these (#178).
SELECT id FROM postings
WHERE company_id = ? AND listing_status = 'open'
ORDER BY id;

-- name: MarkPostingsSeen :exec
-- Records that a sync saw these postings on their board (#176). Not a
-- content change: updated_at and posting_history are left alone. The
-- slice is the only parameter, so sqlc.slice's ordering bug with other
-- bound parameters (see ListActiveApplications) doesn't apply.
UPDATE postings
SET last_seen_at = CURRENT_TIMESTAMP
WHERE id IN (sqlc.slice('ids'));

-- name: SearchPostingsByID :many
-- One page of the postings matching every given filter, in posting ID
-- order, after an optional cursor ID (#218, RFC 0006). NULL means "don't
-- filter on this". Postings of a company the user deleted are never
-- included.
--
-- The inner query applies the filters and counts every match
-- (COUNT(*) OVER ()); the outer one applies the keyset condition and the
-- LIMIT. So total is the whole result's size on every page, while only
-- max_rows rows ever leave the database.
--
-- Application statuses come in as one JSON array read with json_each,
-- not sqlc.slice, which can't be mixed with other bound parameters on
-- sqlite (see ListActiveApplications). Summary columns only (#117); the
-- application side is individually aliased nullable columns, not
-- sqlc.embed (see ListInterestedPostings).
SELECT m.*
FROM (
    SELECT
        postings.id,
        postings.title,
        postings.department,
        postings.location,
        postings.workplace_type,
        postings.application_url,
        postings.listing_status,
        companies.name AS company_name,
        posting_markup.interested_at,
        posting_markup.archived_at,
        applications.id AS application_id,
        applications.status AS application_status,
        postings.published_at,
        applications.notes AS application_notes,
        COUNT(*) OVER () AS total
    FROM postings
    JOIN companies ON companies.id = postings.company_id
    LEFT JOIN posting_markup ON posting_markup.posting_id = postings.id
    LEFT JOIN applications ON applications.posting_id = postings.id AND applications.deleted_at IS NULL
    WHERE companies.deleted_at IS NULL
      AND (sqlc.narg('company_id') IS NULL OR postings.company_id = sqlc.narg('company_id'))
      AND (sqlc.narg('listing_status') IS NULL OR postings.listing_status = sqlc.narg('listing_status'))
      AND (sqlc.arg('include_archived') OR posting_markup.archived_at IS NULL)
      AND (sqlc.narg('has_application') IS NULL OR (applications.id IS NOT NULL) = sqlc.narg('has_application'))
      AND (sqlc.narg('interested') IS NULL OR (posting_markup.interested_at IS NOT NULL) = sqlc.narg('interested'))
      AND (sqlc.narg('statuses') IS NULL OR applications.status IN (SELECT value FROM json_each(sqlc.narg('statuses'))))
) AS m
WHERE sqlc.narg('after_id') IS NULL OR m.id > sqlc.narg('after_id')
ORDER BY m.id
LIMIT sqlc.arg('max_rows');

-- name: SearchPostingsByIDDesc :many
-- SearchPostingsByID newest first: the same filters, in descending posting
-- ID order, before an optional cursor ID (#223). The filters must stay
-- identical to SearchPostingsByID's; a test checks every order matches the
-- same postings. NULL means "don't
-- filter on this". Postings of a company the user deleted are never
-- included.
--
-- The inner query applies the filters and counts every match
-- (COUNT(*) OVER ()); the outer one applies the keyset condition and the
-- LIMIT. So total is the whole result's size on every page, while only
-- max_rows rows ever leave the database.
--
-- Application statuses come in as one JSON array read with json_each,
-- not sqlc.slice, which can't be mixed with other bound parameters on
-- sqlite (see ListActiveApplications). Summary columns only (#117); the
-- application side is individually aliased nullable columns, not
-- sqlc.embed (see ListInterestedPostings).
SELECT m.*
FROM (
    SELECT
        postings.id,
        postings.title,
        postings.department,
        postings.location,
        postings.workplace_type,
        postings.application_url,
        postings.listing_status,
        companies.name AS company_name,
        posting_markup.interested_at,
        posting_markup.archived_at,
        applications.id AS application_id,
        applications.status AS application_status,
        postings.published_at,
        applications.notes AS application_notes,
        COUNT(*) OVER () AS total
    FROM postings
    JOIN companies ON companies.id = postings.company_id
    LEFT JOIN posting_markup ON posting_markup.posting_id = postings.id
    LEFT JOIN applications ON applications.posting_id = postings.id AND applications.deleted_at IS NULL
    WHERE companies.deleted_at IS NULL
      AND (sqlc.narg('company_id') IS NULL OR postings.company_id = sqlc.narg('company_id'))
      AND (sqlc.narg('listing_status') IS NULL OR postings.listing_status = sqlc.narg('listing_status'))
      AND (sqlc.arg('include_archived') OR posting_markup.archived_at IS NULL)
      AND (sqlc.narg('has_application') IS NULL OR (applications.id IS NOT NULL) = sqlc.narg('has_application'))
      AND (sqlc.narg('interested') IS NULL OR (posting_markup.interested_at IS NOT NULL) = sqlc.narg('interested'))
      AND (sqlc.narg('statuses') IS NULL OR applications.status IN (SELECT value FROM json_each(sqlc.narg('statuses'))))
) AS m
WHERE sqlc.narg('before_id') IS NULL OR m.id < sqlc.narg('before_id')
ORDER BY m.id DESC
LIMIT sqlc.arg('max_rows');

-- name: SearchPostingsByPublishedDesc :many
-- SearchPostingsByID newest on the board first (#224): the same filters,
-- ordered by published_at descending, then posting ID descending, with
-- postings that have no published_at last (by ID, descending). The
-- filters must stay identical to SearchPostingsByID's; a test checks every
-- order matches the same postings. NULL means "don't filter on this".
--
-- The cursor is the previous page's last row: cursor_published (NULL when
-- that row had no published_at) and cursor_id. published_at is stored as
-- UTC text in one format with trailing fractional zeros trimmed (00012),
-- so text order is time order, and a bound time.Time is formatted the
-- same way by the connection store.Open makes. Postings of a company
-- the user deleted are never included.
--
-- The inner query applies the filters and counts every match
-- (COUNT(*) OVER ()); the outer one applies the keyset condition and the
-- LIMIT. So total is the whole result's size on every page, while only
-- max_rows rows ever leave the database.
--
-- Application statuses come in as one JSON array read with json_each,
-- not sqlc.slice, which can't be mixed with other bound parameters on
-- sqlite (see ListActiveApplications). Summary columns only (#117); the
-- application side is individually aliased nullable columns, not
-- sqlc.embed (see ListInterestedPostings).
SELECT m.*
FROM (
    SELECT
        postings.id,
        postings.title,
        postings.department,
        postings.location,
        postings.workplace_type,
        postings.application_url,
        postings.listing_status,
        companies.name AS company_name,
        posting_markup.interested_at,
        posting_markup.archived_at,
        applications.id AS application_id,
        applications.status AS application_status,
        postings.published_at,
        applications.notes AS application_notes,
        COUNT(*) OVER () AS total
    FROM postings
    JOIN companies ON companies.id = postings.company_id
    LEFT JOIN posting_markup ON posting_markup.posting_id = postings.id
    LEFT JOIN applications ON applications.posting_id = postings.id AND applications.deleted_at IS NULL
    WHERE companies.deleted_at IS NULL
      AND (sqlc.narg('company_id') IS NULL OR postings.company_id = sqlc.narg('company_id'))
      AND (sqlc.narg('listing_status') IS NULL OR postings.listing_status = sqlc.narg('listing_status'))
      AND (sqlc.arg('include_archived') OR posting_markup.archived_at IS NULL)
      AND (sqlc.narg('has_application') IS NULL OR (applications.id IS NOT NULL) = sqlc.narg('has_application'))
      AND (sqlc.narg('interested') IS NULL OR (posting_markup.interested_at IS NOT NULL) = sqlc.narg('interested'))
      AND (sqlc.narg('statuses') IS NULL OR applications.status IN (SELECT value FROM json_each(sqlc.narg('statuses'))))
) AS m
WHERE sqlc.narg('cursor_id') IS NULL
   OR (sqlc.narg('cursor_published') IS NOT NULL
       AND (m.published_at IS NULL
            OR m.published_at < sqlc.narg('cursor_published')
            OR (m.published_at = sqlc.narg('cursor_published') AND m.id < sqlc.narg('cursor_id'))))
   OR (sqlc.narg('cursor_published') IS NULL AND m.published_at IS NULL AND m.id < sqlc.narg('cursor_id'))
ORDER BY m.published_at IS NULL, m.published_at DESC, m.id DESC
LIMIT sqlc.arg('max_rows');
