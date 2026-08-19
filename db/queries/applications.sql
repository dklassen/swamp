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
