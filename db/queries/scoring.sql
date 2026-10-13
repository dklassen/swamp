-- name: CreateScoringRun :one
INSERT INTO scoring_runs (model, client_name, client_version, profile_sha256, scope)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetScoringRun :one
SELECT * FROM scoring_runs
WHERE id = ?;

-- name: ListPendingPostings :many
-- One page of a run's pending postings, by posting ID after an optional
-- cursor, with how many there are in all (RFC 0009).
--
-- Eligible: open, not archived, not interested, no application, at a
-- company the user hasn't deleted, and let through by that company's
-- filters, matched as the SearchPostings queries match them. Pending: no
-- score in this run, and either the run rescores everything or the
-- posting has no score on its current content. has_score and
-- has_current_score say why; the store names the reason.
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
      AND NOT EXISTS (SELECT 1 FROM posting_scores s WHERE s.run_id = sqlc.arg('run_id') AND s.posting_id = postings.id)
      AND (sqlc.arg('rescore_all') OR NOT EXISTS (SELECT 1 FROM posting_scores s WHERE s.posting_id = postings.id AND s.content_sha256 = postings.content_sha256))
) AS m
WHERE sqlc.narg('after_id') IS NULL OR m.id > sqlc.narg('after_id')
ORDER BY m.id
LIMIT sqlc.arg('max_rows');

-- name: CountScoresInRun :one
SELECT count(*) FROM posting_scores
WHERE run_id = ?;

-- name: ListScoringRuns :many
SELECT * FROM scoring_runs
ORDER BY id DESC
LIMIT ?;
