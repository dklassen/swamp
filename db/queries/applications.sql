-- name: CreateApplication :one
-- Unlike posting_markup, not auto-created for every posting -- an
-- application exists only once the user takes an explicit "start
-- application" action. Relies on column defaults (status='application_started',
-- notes='').
INSERT INTO applications (posting_id)
VALUES (?)
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
