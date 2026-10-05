-- name: CreateDocumentWrite :exec
INSERT INTO document_writes (application_id, document_type, content_sha256, source)
VALUES (?, ?, ?, ?);

-- name: LatestDocumentWrite :one
-- id breaks ties between writes in the same second (CURRENT_TIMESTAMP
-- has one-second resolution); AUTOINCREMENT keeps it increasing.
SELECT * FROM document_writes
WHERE application_id = ? AND document_type = ?
ORDER BY written_at DESC, id DESC
LIMIT 1;
