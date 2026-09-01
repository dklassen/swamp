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
-- sqlc.embed(applications)/sqlc.embed(postings) generate nested
-- Application/Posting fields on the row directly from the schema, rather
-- than us hand-selecting+aliasing individual columns and reconstructing
-- them field-by-field in Go -- verified this works cleanly on this
-- engine (sqlite, sqlc v1.31.1) alongside a plain aliased column, one
-- inner join, no collisions (see decisions.log, ApplicationView).
SELECT sqlc.embed(applications), sqlc.embed(postings), companies.name AS company_name
FROM applications
JOIN postings ON postings.id = applications.posting_id
JOIN companies ON companies.id = postings.company_id
WHERE applications.status NOT IN ('rejected', 'offer_declined')
ORDER BY applications.updated_at DESC;
