-- name: CreateDocumentReview :one
INSERT INTO document_reviews (
    application_id, document_type, cycle, content_snapshot, content_sha256, outcome, notes
) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: CountDocumentReviews :one
SELECT COUNT(*) FROM document_reviews
WHERE application_id = ? AND document_type = ?;

-- name: ListDocumentReviews :many
-- cycle DESC: most recent review first, matching how a human would want
-- to read review history (latest verdict up top).
SELECT * FROM document_reviews
WHERE application_id = ? AND document_type = ?
ORDER BY cycle DESC;
