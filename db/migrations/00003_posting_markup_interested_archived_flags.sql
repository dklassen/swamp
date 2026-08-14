-- +goose Up
-- +goose StatementBegin

-- 'interested' isn't a stage in a pipeline -- it's an independent tag for
-- bookmarking/filtering, and you don't "leave" being interested the way
-- an application progresses through steps. 'archived' is a dismissal,
-- same shape as the deleted_at soft-delete already used for companies
-- and tags elsewhere in this schema. Neither belongs in a mutually
-- exclusive CHECK-constrained enum: a posting can be archived after
-- having been interested, and losing that history the moment it's
-- archived (which the single-enum model forced) is worse than letting
-- both flags coexist.
CREATE TABLE posting_markup_new (
    posting_id    INTEGER PRIMARY KEY REFERENCES postings(id),
    interested_at TIMESTAMP,
    archived_at   TIMESTAMP,
    notes         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- updated_at is the best available signal for "when" since user_status
-- was a single mutable field with no history of its own.
INSERT INTO posting_markup_new (posting_id, interested_at, archived_at, notes, created_at, updated_at)
SELECT posting_id,
       CASE WHEN user_status = 'interested' THEN updated_at END,
       CASE WHEN user_status = 'archived' THEN updated_at END,
       notes, created_at, updated_at
FROM posting_markup;

DROP TABLE posting_markup;
ALTER TABLE posting_markup_new RENAME TO posting_markup;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE posting_markup_old (
    posting_id  INTEGER PRIMARY KEY REFERENCES postings(id),
    user_status TEXT NOT NULL DEFAULT 'new'
                    CHECK (user_status IN ('new', 'interested', 'archived')),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Lossy: a posting that's both interested and archived (impossible under
-- the old single enum, possible now) collapses to 'archived', since
-- archiving is the more recent/decisive action in that case.
INSERT INTO posting_markup_old (posting_id, user_status, notes, created_at, updated_at)
SELECT posting_id,
       CASE
           WHEN archived_at IS NOT NULL THEN 'archived'
           WHEN interested_at IS NOT NULL THEN 'interested'
           ELSE 'new'
       END,
       notes, created_at, updated_at
FROM posting_markup;

DROP TABLE posting_markup;
ALTER TABLE posting_markup_old RENAME TO posting_markup;

-- +goose StatementEnd
