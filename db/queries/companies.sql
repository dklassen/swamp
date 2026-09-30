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

-- name: UpdateCompanyDescription :one
-- Same soft-delete guard as UpdateCompanyName.
UPDATE companies
SET description = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: MarkCompanyFetched :exec
-- Records a successful fetch of this company's postings (see sync.SyncCompany).
UPDATE companies
SET last_fetched_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: AcquireCompanySyncLease :execrows
-- Takes the company's sync lease if it's free or has expired (see
-- store.AcquireSyncLease, #150). Expiry is compared entirely in SQL, so
-- both sides are CURRENT_TIMESTAMP-format UTC text.
UPDATE companies
SET sync_lease_token = sqlc.arg(token), sync_lease_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND (sync_lease_token IS NULL
       OR sync_lease_at < datetime('now', printf('-%d seconds', CAST(sqlc.arg(expiry_seconds) AS INTEGER))));

-- name: ReleaseCompanySyncLease :exec
-- Frees the lease only if token still holds it, so a sync whose lease
-- expired and was taken over can't release the new holder's.
UPDATE companies
SET sync_lease_token = NULL, sync_lease_at = NULL
WHERE id = sqlc.arg(id) AND sync_lease_token = sqlc.arg(token);

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
