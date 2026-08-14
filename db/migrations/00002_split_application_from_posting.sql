-- +goose Up
-- +goose StatementBegin

-- A posting (the ingested Ashby listing) and an application (the user's
-- own pursuit of it) are separate artifacts with separate lifecycles: a
-- posting can be open/closed independent of whether anyone applied, and
-- "applied"/"interviewing"/"rejected" describe the application, not the
-- posting. Splitting them out of posting_markup into their own table.
--
-- 1:1 for now (posting_id is UNIQUE): the common case is one application
-- per posting. Reapplying after a posting closes and reopens under the
-- same source_id is possible but rare/unobserved so far -- deliberately
-- not building for it now (see decisions.log). A real surrogate id (not
-- posting_id reused as the PK, unlike posting_markup) because, unlike
-- posting_markup, applications is itself a parent table now
-- (interview_stages references it) -- reusing posting_id as the PK would
-- make interview_stages.application_id hold posting_id values under an
-- application_id name, which is exactly the kind of silent mixup to
-- avoid.
CREATE TABLE applications (
    id          INTEGER PRIMARY KEY,
    posting_id  INTEGER NOT NULL UNIQUE REFERENCES postings(id),
    status      TEXT NOT NULL DEFAULT 'application_started'
                    CHECK (status IN (
                        'application_started',
                        'application_submitted',
                        'interviewing',
                        'rejected',
                        'offer_received',
                        'offer_accepted',
                        'offer_declined'
                    )),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Backfill: postings whose markup already reflects an application in
-- progress get one, translated to the nearest new status. Existing notes
-- stay on posting_markup as-is (general posting commentary) rather than
-- being guessed apart between posting- and application-level -- this is a
-- deliberate simplification, not an oversight.
INSERT INTO applications (posting_id, status, created_at, updated_at)
SELECT posting_id,
       CASE user_status
           WHEN 'applied'      THEN 'application_submitted'
           WHEN 'interviewing' THEN 'interviewing'
           WHEN 'rejected'     THEN 'rejected'
       END,
       created_at, updated_at
FROM posting_markup
WHERE user_status IN ('applied', 'interviewing', 'rejected');

-- Defensive: a posting with interview_stages but no matching markup
-- status above (shouldn't happen, but interview_stages is about to start
-- requiring an application to attach to).
INSERT INTO applications (posting_id, status, created_at, updated_at)
SELECT DISTINCT posting_id, 'interviewing', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM interview_stages
WHERE posting_id NOT IN (SELECT posting_id FROM applications);

-- posting_markup keeps only lightweight posting-level triage now; SQLite
-- has no ALTER ... ALTER COLUMN for CHECK constraints, so recreate.
CREATE TABLE posting_markup_new (
    posting_id  INTEGER PRIMARY KEY REFERENCES postings(id),
    user_status TEXT NOT NULL DEFAULT 'new'
                    CHECK (user_status IN ('new', 'interested', 'archived')),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO posting_markup_new (posting_id, user_status, notes, created_at, updated_at)
SELECT posting_id,
       CASE WHEN user_status IN ('applied', 'interviewing', 'rejected') THEN 'interested'
            ELSE user_status
       END,
       notes, created_at, updated_at
FROM posting_markup;

DROP TABLE posting_markup;
ALTER TABLE posting_markup_new RENAME TO posting_markup;

-- interview_stages now belongs to an application, not a posting directly.
CREATE TABLE interview_stages_new (
    id              INTEGER PRIMARY KEY,
    application_id  INTEGER NOT NULL REFERENCES applications(id),
    sequence        INTEGER NOT NULL,
    name            TEXT NOT NULL,
    stage_date      DATE,
    outcome         TEXT NOT NULL DEFAULT 'pending'
                        CHECK (outcome IN ('pending', 'passed', 'failed')),
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Joined through applications.posting_id: the backfill above guarantees
-- at most one application per posting_id that has interview_stages rows.
INSERT INTO interview_stages_new (id, application_id, sequence, name, stage_date, outcome, notes, created_at, updated_at)
SELECT ist.id, a.id, ist.sequence, ist.name, ist.stage_date, ist.outcome, ist.notes, ist.created_at, ist.updated_at
FROM interview_stages ist
JOIN applications a ON a.posting_id = ist.posting_id;

DROP TABLE interview_stages;
ALTER TABLE interview_stages_new RENAME TO interview_stages;
CREATE INDEX idx_interview_stages_application_id ON interview_stages(application_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE interview_stages_old (
    id          INTEGER PRIMARY KEY,
    posting_id  INTEGER NOT NULL REFERENCES postings(id),
    sequence    INTEGER NOT NULL,
    name        TEXT NOT NULL,
    stage_date  DATE,
    outcome     TEXT NOT NULL DEFAULT 'pending'
                    CHECK (outcome IN ('pending', 'passed', 'failed')),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO interview_stages_old (id, posting_id, sequence, name, stage_date, outcome, notes, created_at, updated_at)
SELECT ist.id, a.posting_id, ist.sequence, ist.name, ist.stage_date, ist.outcome, ist.notes, ist.created_at, ist.updated_at
FROM interview_stages ist
JOIN applications a ON a.id = ist.application_id;

DROP TABLE interview_stages;
ALTER TABLE interview_stages_old RENAME TO interview_stages;
CREATE INDEX idx_interview_stages_posting_id ON interview_stages(posting_id);

CREATE TABLE posting_markup_old (
    posting_id  INTEGER PRIMARY KEY REFERENCES postings(id),
    user_status TEXT NOT NULL DEFAULT 'new'
                    CHECK (user_status IN
                        ('new', 'interested', 'applied', 'interviewing', 'rejected', 'archived')),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Lossy: an application's status wins over posting_markup's own status
-- when both exist, since it carries more information; the new
-- offer_*/application_* values collapse onto their nearest v1 equivalent.
INSERT INTO posting_markup_old (posting_id, user_status, notes, created_at, updated_at)
SELECT pm.posting_id,
       COALESCE(
           CASE a.status
               WHEN 'application_started'   THEN 'interested'
               WHEN 'application_submitted' THEN 'applied'
               WHEN 'interviewing'          THEN 'interviewing'
               WHEN 'rejected'              THEN 'rejected'
               WHEN 'offer_received'        THEN 'interviewing'
               WHEN 'offer_accepted'        THEN 'interviewing'
               WHEN 'offer_declined'        THEN 'interviewing'
           END,
           pm.user_status
       ),
       pm.notes, pm.created_at, pm.updated_at
FROM posting_markup pm
LEFT JOIN applications a ON a.posting_id = pm.posting_id;

DROP TABLE posting_markup;
ALTER TABLE posting_markup_old RENAME TO posting_markup;

DROP TABLE applications;

-- +goose StatementEnd
