-- name: CreatePostingMarkup :one
-- Not exposed directly on the Store API: every posting must have exactly
-- one markup row, so Store creates it as part of UpsertPosting rather than
-- leaving callers to remember a second call. Relies on column defaults
-- (interested_at=NULL, archived_at=NULL, notes='').
INSERT INTO posting_markup (posting_id)
VALUES (?)
RETURNING *;

-- name: GetPostingMarkup :one
SELECT * FROM posting_markup
WHERE posting_id = ?;

-- name: UnmarkPostingInterested :one
UPDATE posting_markup
SET interested_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;

-- name: SetPostingInterested :one
-- Sets interested_at and also clears archived_at: the TUI treats
-- interested/archived as mutually exclusive from the user's perspective
-- (pressing "interested" while archived switches state rather than
-- stacking), even though the schema itself allows both to be set.
UPDATE posting_markup
SET interested_at = CURRENT_TIMESTAMP, archived_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;

-- name: UnarchivePosting :one
UPDATE posting_markup
SET archived_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;

-- name: SetPostingArchived :one
-- See SetPostingInterested -- same mutual-exclusivity reasoning, mirrored.
UPDATE posting_markup
SET archived_at = CURRENT_TIMESTAMP, interested_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;

-- name: UpdatePostingMarkupNotes :one
UPDATE posting_markup
SET notes = ?, updated_at = CURRENT_TIMESTAMP
WHERE posting_id = ?
RETURNING *;
