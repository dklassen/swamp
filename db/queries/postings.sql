-- name: CreatePosting :one
INSERT INTO postings (
    company_id, source, source_id, title, department, team, location,
    employment_type, workplace_type, description_html, description_text,
    job_url, application_url, published_at, raw_payload
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdatePosting :one
-- Updates ingested content fields only. Deliberately does not touch
-- listing_status: transitions between open/closed are explicit domain
-- decisions (see MarkPostingClosed/MarkPostingReopened), not a side effect
-- of re-ingesting content. Does not touch company_id: a given (source,
-- source_id) is assumed to belong to the same company for its lifetime.
UPDATE postings
SET title = ?,
    department = ?,
    team = ?,
    location = ?,
    employment_type = ?,
    workplace_type = ?,
    description_html = ?,
    description_text = ?,
    job_url = ?,
    application_url = ?,
    published_at = ?,
    raw_payload = ?,
    last_seen_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: GetPosting :one
SELECT * FROM postings
WHERE id = ?;

-- name: GetPostingBySourceAndSourceID :one
SELECT * FROM postings
WHERE source = ? AND source_id = ?;

-- name: ListPostingsByCompany :many
-- id DESC is a tiebreaker: a single sync inserts many rows within the same
-- CURRENT_TIMESTAMP second (sqlite has only second resolution), so
-- first_seen_at alone leaves ties with no guaranteed order.
SELECT * FROM postings
WHERE company_id = ?
ORDER BY first_seen_at DESC, id DESC;

-- name: MarkPostingClosed :exec
UPDATE postings
SET listing_status = 'closed', updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: MarkPostingReopened :exec
UPDATE postings
SET listing_status = 'open', updated_at = CURRENT_TIMESTAMP
WHERE id = ?;
