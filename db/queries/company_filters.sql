-- name: CreateCompanyFilter :one
INSERT INTO company_filters (company_id, field, value)
VALUES (?, ?, ?)
RETURNING *;

-- name: ListCompanyFilters :many
SELECT * FROM company_filters
WHERE company_id = ?
ORDER BY id;

-- name: DeleteCompanyFilters :exec
DELETE FROM company_filters
WHERE company_id = ?;
