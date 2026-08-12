-- name: CreatePostingMarkup :one
-- Not exposed directly on the Store API: every posting must have exactly
-- one markup row, so Store creates it as part of UpsertPosting rather than
-- leaving callers to remember a second call. Relies on column defaults
-- (user_status='new', notes='').
INSERT INTO posting_markup (posting_id)
VALUES (?)
RETURNING *;

-- name: GetPostingMarkup :one
SELECT * FROM posting_markup
WHERE posting_id = ?;

-- name: UpdatePostingMarkupStatus :one
UPDATE posting_markup
SET user_status = ?, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;

-- name: UpdatePostingMarkupNotes :one
UPDATE posting_markup
SET notes = ?, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;
