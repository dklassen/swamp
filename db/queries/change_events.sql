-- name: ChangeEventsAfter :many
SELECT * FROM change_events
WHERE id > ?
ORDER BY id;

-- name: LatestChangeEventID :one
SELECT CAST(coalesce(max(id), 0) AS INTEGER) AS id FROM change_events;
