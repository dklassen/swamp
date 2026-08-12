-- name: CreateTag :one
INSERT INTO tags (name)
VALUES (?)
RETURNING *;

-- name: GetTagByName :one
-- Not filtered on deleted_at: used for existence checks (e.g. before
-- creating) where callers need to know about a soft-deleted tag too, not
-- just active ones.
SELECT * FROM tags
WHERE name = ?;

-- name: ListTags :many
-- Active tags only: this is the pick list for tagging postings, and a
-- soft-deleted tag shouldn't be offered for new use.
SELECT * FROM tags
WHERE deleted_at IS NULL
ORDER BY name;

-- name: SoftDeleteTag :exec
UPDATE tags
SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: AddTagToPosting :exec
INSERT INTO posting_tags (posting_id, tag_id)
VALUES (?, ?);

-- name: RemoveTagFromPosting :exec
DELETE FROM posting_tags
WHERE posting_id = ? AND tag_id = ?;

-- name: ListTagsForPosting :many
-- Not filtered on deleted_at: a posting's existing tag associations are
-- historical record and should keep showing even if the tag itself was
-- later soft-deleted (see schema comment on posting_tags).
SELECT t.* FROM tags t
JOIN posting_tags pt ON pt.tag_id = t.id
WHERE pt.posting_id = ?
ORDER BY t.name;
