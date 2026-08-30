-- name: CreateCompany :one
INSERT INTO companies (name, source, source_ref)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetCompany :one
SELECT * FROM companies
WHERE id = ? AND deleted_at IS NULL;

-- name: GetCompanyBySourceAndSourceRef :one
-- Deliberately ignores deleted_at: source+source_ref is UNIQUE across all
-- rows regardless of soft-delete state, so re-adding a company (CreateCompany)
-- needs to find a soft-deleted match too, in order to restore it instead of
-- violating the UNIQUE constraint with a duplicate insert.
SELECT * FROM companies
WHERE source = ? AND source_ref = ?;

-- name: RestoreCompanyWithName :one
UPDATE companies
SET name = ?, deleted_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: UpdateCompanyName :one
-- Excludes soft-deleted rows, same guard as GetCompany -- editing a
-- deleted company isn't a supported action.
UPDATE companies
SET name = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

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
