-- name: CreateCompany :one
INSERT INTO companies (name, source, source_ref)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetCompany :one
SELECT * FROM companies
WHERE id = ? AND deleted_at IS NULL;

-- name: ListActiveCompanies :many
SELECT * FROM companies
WHERE deleted_at IS NULL
ORDER BY name;

-- name: SoftDeleteCompany :exec
UPDATE companies
SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: RestoreCompany :exec
UPDATE companies
SET deleted_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;
