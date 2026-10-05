-- name: CreateApplicationStatusHistory :exec
-- Written only by store.recordApplicationStatus, inside the same
-- transaction as the status change it records (#162).
INSERT INTO application_status_history (application_id, status, changed_by)
VALUES (?, ?, ?);

-- name: ListApplicationStatusHistory :many
-- Oldest first. id breaks ties between changes in the same second
-- (CURRENT_TIMESTAMP has one-second resolution).
SELECT * FROM application_status_history
WHERE application_id = ?
ORDER BY changed_at, id;

-- name: LatestApplicationStatusChange :one
-- When the application entered its current status: every status write
-- records a row (#162), so the newest is the current status's.
SELECT * FROM application_status_history
WHERE application_id = ?
ORDER BY changed_at DESC, id DESC
LIMIT 1;

-- name: DeleteApplicationStatusHistory :exec
-- Only for store.DeleteApplication (#232): the history is append-only
-- otherwise.
DELETE FROM application_status_history
WHERE application_id = ?;
