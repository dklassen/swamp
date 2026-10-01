-- name: SavePostingApplicationForm :exec
-- Replaces any earlier fetch of the same posting's form.
INSERT INTO posting_application_forms (posting_id, form)
VALUES (?, ?)
ON CONFLICT (posting_id) DO UPDATE SET form = excluded.form, fetched_at = CURRENT_TIMESTAMP;

-- name: GetPostingApplicationForm :one
SELECT * FROM posting_application_forms
WHERE posting_id = ?;
