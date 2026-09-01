-- name: CreateApplication :one
-- Unlike posting_markup, not auto-created for every posting -- an
-- application exists only once the user takes an explicit "start
-- application" action. status is supplied explicitly by the caller
-- (store.CreateApplication passes store.ApplicationStatusStarted.String())
-- rather than relying on a DB default -- the initial value is the
-- application's decision, not the schema's (see db/migrations/00004_...,
-- PR #17 review). notes still relies on its own column default ('').
INSERT INTO applications (posting_id, status)
VALUES (?, ?)
RETURNING *;

-- name: GetApplication :one
SELECT * FROM applications
WHERE posting_id = ?;

-- name: UpdateApplicationStatus :one
UPDATE applications
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;

-- name: UpdateApplicationNotes :one
UPDATE applications
SET notes = ?, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;

-- name: ListActiveApplications :many
-- Applications not at a terminal dead-end status (rejected,
-- offer_declined), joined with their posting and company name -- feeds
-- the active-applications TUI screen (#43). Unlike ListInterestedPostings
-- this is an inner join on applications (an application always exists
-- for every row here), ordered most-recently-changed first so whatever
-- moved last surfaces at the top.
--
-- Selects every applications column (aliased to avoid colliding with
-- postings' own id/created_at/updated_at from postings.*) so the Go side
-- can build a complete, honest store.Application via applicationFromRow
-- -- not a partial one that happens to share a name with the real thing
-- (see decisions.log, ApplicationView).
SELECT
    postings.*,
    companies.name AS company_name,
    applications.id AS application_id,
    applications.status AS application_status,
    applications.notes AS application_notes,
    applications.created_at AS application_created_at,
    applications.updated_at AS application_updated_at
FROM applications
JOIN postings ON postings.id = applications.posting_id
JOIN companies ON companies.id = postings.company_id
WHERE applications.status NOT IN ('rejected', 'offer_declined')
ORDER BY applications.updated_at DESC;
