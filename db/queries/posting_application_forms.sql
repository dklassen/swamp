-- name: SavePostingApplicationForm :exec
-- Replaces any earlier fetch of the same posting's form.
INSERT INTO posting_application_forms (posting_id, form)
VALUES (?, ?)
ON CONFLICT (posting_id) DO UPDATE SET form = excluded.form, fetched_at = CURRENT_TIMESTAMP;

-- name: GetPostingApplicationForm :one
SELECT * FROM posting_application_forms
WHERE posting_id = ?;

-- name: ListOpenPostingsWithoutForm :many
SELECT * FROM postings
WHERE company_id = ? AND listing_status = 'open'
  AND NOT EXISTS (SELECT 1 FROM posting_application_forms WHERE posting_application_forms.posting_id = postings.id)
ORDER BY id;

-- name: InsertPostingApplicationForm :exec
-- Leaves a stored form in place.
INSERT INTO posting_application_forms (posting_id, form)
VALUES (?, ?)
ON CONFLICT (posting_id) DO NOTHING;
