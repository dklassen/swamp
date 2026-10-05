-- name: CreateDocumentExport :exec
INSERT INTO document_exports (application_id, document_type, content_sha256, path)
VALUES (?, ?, ?, ?);

-- name: LatestDocumentExport :one
-- id breaks ties between exports in the same second (CURRENT_TIMESTAMP
-- has one-second resolution).
SELECT * FROM document_exports
WHERE application_id = ? AND document_type = ?
ORDER BY exported_at DESC, id DESC
LIMIT 1;

-- name: DeleteDocumentExportsForApplication :exec
DELETE FROM document_exports
WHERE application_id = ?;
