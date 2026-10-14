-- name: ListPendingPostings :many
-- One page of the pending postings, by posting ID after an optional
-- cursor, with how many there are in all.
--
-- Eligible: open, not archived, not interested, no application, at a
-- company the user hasn't deleted, and let through by that company's
-- filters, matched as the SearchPostings queries match them. Pending: no
-- score on its current content, or for a rescore, none on its current
-- content against rescore_profile. has_score and has_current_score say
-- why; the store names the reason.
--
-- The cursor is a narg, as in the SearchPostings queries: with sqlc.arg there,
-- sqlc can't resolve the alias m.
SELECT m.*
FROM (
    SELECT
        postings.id,
        postings.title,
        companies.name AS company_name,
        postings.department,
        postings.location,
        postings.workplace_type,
        postings.published_at,
        postings.content_sha256,
        EXISTS (SELECT 1 FROM posting_scores s WHERE s.posting_id = postings.id) AS has_score,
        EXISTS (SELECT 1 FROM posting_scores s WHERE s.posting_id = postings.id AND s.content_sha256 = postings.content_sha256) AS has_current_score,
        COUNT(*) OVER () AS total
    FROM postings
    JOIN companies ON companies.id = postings.company_id
    LEFT JOIN posting_markup ON posting_markup.posting_id = postings.id
    WHERE companies.deleted_at IS NULL
      AND postings.listing_status = 'open'
      AND posting_markup.archived_at IS NULL
      AND posting_markup.interested_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM applications a WHERE a.posting_id = postings.id AND a.deleted_at IS NULL)
      AND (NOT EXISTS (SELECT 1 FROM company_filters cf WHERE cf.company_id = postings.company_id AND cf.field = 'department')
           OR EXISTS (SELECT 1 FROM company_filters cf WHERE cf.company_id = postings.company_id AND cf.field = 'department' AND equal_fold(cf.value, postings.department)))
      AND (NOT EXISTS (SELECT 1 FROM company_filters cf WHERE cf.company_id = postings.company_id AND cf.field = 'location')
           OR EXISTS (SELECT 1 FROM company_filters cf WHERE cf.company_id = postings.company_id AND cf.field = 'location' AND equal_fold(cf.value, postings.location)))
      AND NOT EXISTS (
          SELECT 1 FROM posting_scores s
          WHERE s.posting_id = postings.id
            AND s.content_sha256 = postings.content_sha256
            AND (sqlc.narg('rescore_profile') IS NULL OR s.profile_sha256 = sqlc.narg('rescore_profile'))
      )
) AS m
WHERE sqlc.narg('after_id') IS NULL OR m.id > sqlc.narg('after_id')
ORDER BY m.id
LIMIT sqlc.arg('max_rows');

-- name: GetPostingContentSHA256 :one
SELECT content_sha256 FROM postings
WHERE id = ?;

-- name: HasRepeatScore :one
-- Whether the posting already has a score that a new one would only repeat:
-- on the same content, against the same profile, from the same model.
SELECT EXISTS (
    SELECT 1 FROM posting_scores
    WHERE posting_id = ? AND content_sha256 = ? AND profile_sha256 = ? AND model = ?
);

-- name: CreatePostingScore :exec
INSERT INTO posting_scores (posting_id, score, reason, content_sha256, profile_sha256, model, client_name, client_version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);
