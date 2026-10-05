-- name: CreateInterviewStage :one
INSERT INTO interview_stages (application_id, sequence, name, stage_date, notes)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListInterviewStagesByApplication :many
SELECT * FROM interview_stages
WHERE application_id = ?
ORDER BY sequence;

-- name: UpdateInterviewStageOutcome :one
UPDATE interview_stages
SET outcome = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: UpdateInterviewStage :one
UPDATE interview_stages
SET sequence = ?,
    name = ?,
    stage_date = ?,
    outcome = ?,
    notes = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: DeleteInterviewStage :exec
DELETE FROM interview_stages
WHERE id = ?;
