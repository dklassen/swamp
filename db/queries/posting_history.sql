-- name: CreatePostingHistory :one
INSERT INTO posting_history (posting_id, change_type, snapshot)
VALUES (?, ?, ?)
RETURNING *;

-- name: ListPostingHistoryByPosting :many
SELECT * FROM posting_history
WHERE posting_id = ?
ORDER BY recorded_at;
