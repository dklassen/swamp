-- name: CreateApplicationStatusHistory :exec
-- Written only by store.recordApplicationStatus, inside the same
-- transaction as the status change it records (#162).
INSERT INTO application_status_history (application_id, status)
VALUES (?, ?);

-- name: ListApplicationStatusHistory :many
-- Oldest first. id breaks ties between changes in the same second
-- (CURRENT_TIMESTAMP has one-second resolution).
SELECT * FROM application_status_history
WHERE application_id = ?
ORDER BY changed_at, id;
