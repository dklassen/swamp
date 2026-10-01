package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/store/db"
)

// IngestedFields is a posting's content as ingested from its source --
// exactly the fields store.Posting and CreatePostingParams share, and
// exactly the fields a re-fetch needs to compare to detect a real
// content change (see IngestPosting). Pulled into one type so there's a
// single place defining "what counts as a posting's content," rather
// than that field list being hand-copied at every site that needs it
// (see decisions.log, #57).
//
// json tags pin the field names exactly as they already serialize today
// (Go's default reflect-based names) rather than changing them -- the
// point is making a future Go-side rename require an explicit tag edit
// to also change the JSON contract an external LLM agent reads
// (.agents/skills/apply-to-posting/SKILL.md), not changing that contract
// now (see decisions.log, #59).
type IngestedFields struct {
	Title           string       `json:"Title"`
	Department      string       `json:"Department"`
	Team            string       `json:"Team"`
	Location        string       `json:"Location"`
	EmploymentType  string       `json:"EmploymentType"`
	WorkplaceType   string       `json:"WorkplaceType"`
	DescriptionHTML string       `json:"DescriptionHTML"`
	DescriptionText string       `json:"DescriptionText"`
	JobURL          string       `json:"JobURL"`
	ApplicationURL  string       `json:"ApplicationURL"`
	PublishedAt     OptionalTime `json:"PublishedAt"`
	RawPayload      string       `json:"RawPayload"`
}

// Posting is a source-agnostic job posting: canonical fields are
// normalized across boards, RawPayload preserves the original response for
// anything not (yet) promoted to a canonical field.
//
// IngestedFields is deliberately left without its own json tag: an
// anonymous field with no tag stays promoted (its fields serialize
// directly into this struct's own JSON object, matching how the current
// agent hand-off JSON already looks), where a tag would instead nest it
// under an "IngestedFields" key and silently break that contract (see
// decisions.log, #59).
type Posting struct {
	ID        int64  `json:"ID"`
	CompanyID int64  `json:"CompanyID"`
	Source    string `json:"Source"`
	SourceID  string `json:"SourceID"`
	IngestedFields
	ListingStatus string    `json:"ListingStatus"`
	FirstSeenAt   time.Time `json:"FirstSeenAt"`
	LastSeenAt    time.Time `json:"LastSeenAt"`
	CreatedAt     time.Time `json:"CreatedAt"`
	UpdatedAt     time.Time `json:"UpdatedAt"`
}

// CreatePostingParams are the ingested fields for a posting, plus the
// identity fields needed to place it. This is the shape whoever maps a
// source-specific posting (e.g. a jobboard.Posting) into store is
// expected to populate; store itself has no knowledge of any specific
// job board.
type CreatePostingParams struct {
	CompanyID int64
	Source    string
	SourceID  string
	IngestedFields
}

func postingFromRow(row db.Posting) Posting {
	return Posting{
		ID:            row.ID,
		CompanyID:     row.CompanyID,
		Source:        row.Source,
		SourceID:      row.SourceID,
		ListingStatus: row.ListingStatus,
		FirstSeenAt:   row.FirstSeenAt,
		LastSeenAt:    row.LastSeenAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
		IngestedFields: IngestedFields{
			Title:           row.Title,
			Department:      row.Department,
			Team:            row.Team,
			Location:        row.Location,
			EmploymentType:  row.EmploymentType,
			WorkplaceType:   row.WorkplaceType,
			DescriptionHTML: row.DescriptionHtml,
			DescriptionText: row.DescriptionText,
			JobURL:          row.JobUrl,
			ApplicationURL:  row.ApplicationUrl,
			PublishedAt:     OptionalTime{Time: row.PublishedAt.Time},
			RawPayload:      row.RawPayload,
		},
	}
}

// nullTime converts an optional *time.Time into the DB layer's nullable
// representation -- shared with InterviewStage.StageDate, which (unlike
// IngestedFields.PublishedAt below) is a genuinely pointer-optional field,
// out of #67's scope.
func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// nullPublishedAt is nullTime for IngestedFields.PublishedAt specifically:
// the zero time means absent, since PublishedAt isn't pointer-optional
// (see decisions.log, #67).
func nullPublishedAt(t OptionalTime) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.Time, Valid: true}
}

// UpsertPosting inserts a new posting or updates the existing one for the
// same (source, source_id) -- IngestPosting, returning just the posting.
func (s *Store) UpsertPosting(ctx context.Context, params CreatePostingParams) (Posting, error) {
	result, err := s.IngestPosting(ctx, params)
	return result.Posting, err
}

// IngestResult reports what IngestPosting did.
type IngestResult struct {
	Posting Posting
	// Created: the posting didn't exist and was inserted.
	Created bool
	// Updated: the posting existed with different content, which was
	// replaced and recorded as a "content_updated" history row.
	Updated bool
}

// IngestPosting saves a fetched posting, all in one transaction (#148):
//   - not stored yet: inserts it, along with its markup row, so every
//     posting always has exactly one markup row and callers never have to
//     remember a second call to create it;
//   - stored with different content: records the stored version as a
//     "content_updated" history row, then updates it;
//   - stored with the same content: changes nothing.
//
// Comparing against the row read inside the transaction, not one the
// caller read earlier, is what keeps an overlapping sync of the same
// posting from counting it as created twice or recording one change
// twice. An update keeps the row's listing_status untouched (see the
// UpdatePosting query comment for why).
func (s *Store) IngestPosting(ctx context.Context, params CreatePostingParams) (IngestResult, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return IngestResult{}, fmt.Errorf("store: begin ingest posting tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.queries.WithTx(tx)

	existing, err := qtx.GetPostingBySourceAndSourceID(ctx, db.GetPostingBySourceAndSourceIDParams{
		Source:   params.Source,
		SourceID: params.SourceID,
	})

	var result IngestResult
	var row db.Posting
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result.Created = true
		row, err = qtx.CreatePosting(ctx, db.CreatePostingParams{
			CompanyID:       params.CompanyID,
			Source:          params.Source,
			SourceID:        params.SourceID,
			Title:           params.Title,
			Department:      params.Department,
			Team:            params.Team,
			Location:        params.Location,
			EmploymentType:  params.EmploymentType,
			WorkplaceType:   params.WorkplaceType,
			DescriptionHtml: params.DescriptionHTML,
			DescriptionText: params.DescriptionText,
			JobUrl:          params.JobURL,
			ApplicationUrl:  params.ApplicationURL,
			PublishedAt:     nullPublishedAt(params.PublishedAt),
			RawPayload:      params.RawPayload,
		})
		if err != nil {
			return IngestResult{}, fmt.Errorf("store: create posting: %w", err)
		}
		if _, err := qtx.CreatePostingMarkup(ctx, row.ID); err != nil {
			return IngestResult{}, fmt.Errorf("store: create posting markup: %w", err)
		}
	case err != nil:
		return IngestResult{}, fmt.Errorf("store: get posting by source: %w", err)
	default:
		stored := postingFromRow(existing)
		if cmp.Equal(stored.IngestedFields, params.IngestedFields) {
			return IngestResult{Posting: stored}, nil
		}
		snapshot, err := json.Marshal(stored)
		if err != nil {
			return IngestResult{}, fmt.Errorf("store: marshal posting snapshot: %w", err)
		}
		if _, err := qtx.CreatePostingHistory(ctx, db.CreatePostingHistoryParams{
			PostingID:  existing.ID,
			ChangeType: "content_updated",
			Snapshot:   string(snapshot),
		}); err != nil {
			return IngestResult{}, fmt.Errorf("store: record posting content updated: %w", err)
		}
		result.Updated = true
		row, err = qtx.UpdatePosting(ctx, db.UpdatePostingParams{
			ID:              existing.ID,
			Title:           params.Title,
			Department:      params.Department,
			Team:            params.Team,
			Location:        params.Location,
			EmploymentType:  params.EmploymentType,
			WorkplaceType:   params.WorkplaceType,
			DescriptionHtml: params.DescriptionHTML,
			DescriptionText: params.DescriptionText,
			JobUrl:          params.JobURL,
			ApplicationUrl:  params.ApplicationURL,
			PublishedAt:     nullPublishedAt(params.PublishedAt),
			RawPayload:      params.RawPayload,
		})
		if err != nil {
			return IngestResult{}, fmt.Errorf("store: update posting: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return IngestResult{}, fmt.Errorf("store: commit ingest posting tx: %w", err)
	}
	result.Posting = postingFromRow(row)
	return result, nil
}

func (s *Store) GetPosting(ctx context.Context, id int64) (Posting, error) {
	row, err := s.queries.GetPosting(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Posting{}, ErrNotFound
		}
		return Posting{}, err
	}
	return postingFromRow(row), nil
}

func (s *Store) GetPostingBySourceAndSourceID(ctx context.Context, source, sourceID string) (Posting, error) {
	row, err := s.queries.GetPostingBySourceAndSourceID(ctx, db.GetPostingBySourceAndSourceIDParams{
		Source:   source,
		SourceID: sourceID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Posting{}, ErrNotFound
		}
		return Posting{}, err
	}
	return postingFromRow(row), nil
}

func (s *Store) ListPostingsByCompany(ctx context.Context, companyID int64) ([]Posting, error) {
	rows, err := s.queries.ListPostingsByCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}
	postings := make([]Posting, len(rows))
	for i, row := range rows {
		postings[i] = postingFromRow(row)
	}
	return postings, nil
}

// MarkPostingClosed marks a posting closed, e.g. because it no longer
// appeared in the most recent fetch of its company's board.
func (s *Store) MarkPostingClosed(ctx context.Context, id int64) error {
	return s.queries.MarkPostingClosed(ctx, id)
}

// ClosePostingResult reports what ClosePosting changed.
type ClosePostingResult struct {
	// Closed is false when the posting was already closed, e.g. by an
	// overlapping sync; nothing else is changed then either.
	Closed bool
	// ApplicationClosed is true when the posting's application was moved
	// to posting_closed.
	ApplicationClosed bool
}

// ClosePosting closes an open posting, records a "closed" posting_history
// snapshot of it, and moves its application (if any) to posting_closed
// when the application's status is one of closeApplicationFrom, recording
// that in its status history (#162) -- all in one transaction (#147).
// Doing them as separate commits let an interruption close the posting
// but not its application, and the next sync, which only looks at open
// postings, never came back to it.
//
// Closing is conditional on the posting still being open, so a second,
// overlapping sync closing the same posting changes nothing and records
// no second history row. Which application statuses a closing posting
// ends is the caller's policy (sync's earlyApplicationStatuses, #105);
// store only applies it.
func (s *Store) ClosePosting(ctx context.Context, postingID int64, closeApplicationFrom []ApplicationStatus) (ClosePostingResult, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return ClosePostingResult{}, fmt.Errorf("store: begin close posting tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := closePosting(ctx, s.queries.WithTx(tx), postingID, closeApplicationFrom)
	if err != nil {
		return ClosePostingResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return ClosePostingResult{}, fmt.Errorf("store: commit close posting tx: %w", err)
	}
	return result, nil
}

// closePosting is ClosePosting inside the caller's transaction, shared
// with SoftDeleteCompany.
func closePosting(ctx context.Context, qtx *db.Queries, postingID int64, closeApplicationFrom []ApplicationStatus) (ClosePostingResult, error) {
	row, err := qtx.GetPosting(ctx, postingID)
	if err != nil {
		return ClosePostingResult{}, fmt.Errorf("store: get posting to close: %w", err)
	}
	changed, err := qtx.ClosePostingIfOpen(ctx, postingID)
	if err != nil {
		return ClosePostingResult{}, fmt.Errorf("store: close posting: %w", err)
	}
	if changed == 0 {
		return ClosePostingResult{}, nil
	}

	snapshot, err := json.Marshal(postingFromRow(row))
	if err != nil {
		return ClosePostingResult{}, fmt.Errorf("store: marshal posting snapshot: %w", err)
	}
	if _, err := qtx.CreatePostingHistory(ctx, db.CreatePostingHistoryParams{
		PostingID:  postingID,
		ChangeType: "closed",
		Snapshot:   string(snapshot),
	}); err != nil {
		return ClosePostingResult{}, fmt.Errorf("store: record posting closed: %w", err)
	}

	result := ClosePostingResult{Closed: true}
	appRow, err := qtx.GetApplication(ctx, postingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Most postings are never applied to; nothing more to close.
	case err != nil:
		return ClosePostingResult{}, fmt.Errorf("store: get application for closed posting: %w", err)
	default:
		application, err := applicationFromRow(appRow)
		if err != nil {
			return ClosePostingResult{}, err
		}
		if slices.Contains(closeApplicationFrom, application.Status) {
			if _, err := qtx.UpdateApplicationStatus(ctx, db.UpdateApplicationStatusParams{
				PostingID: postingID,
				Status:    sql.NullString{String: ApplicationStatusPostingClosed.String(), Valid: true},
			}); err != nil {
				return ClosePostingResult{}, fmt.Errorf("store: close application for closed posting: %w", err)
			}
			if err := recordApplicationStatus(ctx, qtx, application.ID, ApplicationStatusPostingClosed, StatusChangedBySync); err != nil {
				return ClosePostingResult{}, err
			}
			result.ApplicationClosed = true
		}
	}
	return result, nil
}

// ReopenPostingResult reports what ReopenPosting changed.
type ReopenPostingResult struct {
	// Reopened is false when the posting was already open, e.g. reopened
	// by an overlapping sync; nothing else is changed then either.
	Reopened bool
	// ApplicationRestored is true when the posting's application was moved
	// back from posting_closed to the status it had before.
	ApplicationRestored bool
}

// ReopenPosting marks a closed posting open again, e.g. because it
// reappeared in a fetch, and records a "reopened" posting_history
// snapshot of it as it was just before -- in one transaction (#148).
// Reopening changes nothing when the posting was already open, e.g.
// reopened by an overlapping sync, and nothing is recorded either.
//
// In the same transaction it undoes what closing the posting did to its
// application (#174): an application sync moved to posting_closed goes
// back to the status it had before, recorded as sync's change. Only if
// sync's close is still the application's latest change -- a
// posting_closed the user set, or any status the user set since, is
// left alone, and so is a close recorded before history said who made
// it (00018).
func (s *Store) ReopenPosting(ctx context.Context, postingID int64) (ReopenPostingResult, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return ReopenPostingResult{}, fmt.Errorf("store: begin reopen posting tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.queries.WithTx(tx)

	row, err := qtx.GetPosting(ctx, postingID)
	if err != nil {
		return ReopenPostingResult{}, fmt.Errorf("store: get posting to reopen: %w", err)
	}
	changed, err := qtx.ReopenPostingIfClosed(ctx, postingID)
	if err != nil {
		return ReopenPostingResult{}, fmt.Errorf("store: reopen posting: %w", err)
	}
	if changed == 0 {
		return ReopenPostingResult{}, nil
	}

	snapshot, err := json.Marshal(postingFromRow(row))
	if err != nil {
		return ReopenPostingResult{}, fmt.Errorf("store: marshal posting snapshot: %w", err)
	}
	if _, err := qtx.CreatePostingHistory(ctx, db.CreatePostingHistoryParams{
		PostingID:  postingID,
		ChangeType: "reopened",
		Snapshot:   string(snapshot),
	}); err != nil {
		return ReopenPostingResult{}, fmt.Errorf("store: record posting reopened: %w", err)
	}

	restored, err := restoreSyncClosedApplication(ctx, qtx, postingID)
	if err != nil {
		return ReopenPostingResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return ReopenPostingResult{}, fmt.Errorf("store: commit reopen posting tx: %w", err)
	}
	return ReopenPostingResult{Reopened: true, ApplicationRestored: restored}, nil
}

// restoreSyncClosedApplication is ReopenPosting's application half, in
// its transaction: if the posting's application is at posting_closed
// because sync put it there, and nothing has changed it since, put it
// back to the status before that.
func restoreSyncClosedApplication(ctx context.Context, qtx *db.Queries, postingID int64) (bool, error) {
	appRow, err := qtx.GetApplication(ctx, postingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("store: get application for reopened posting: %w", err)
	}
	history, err := qtx.ListApplicationStatusHistory(ctx, appRow.ID)
	if err != nil {
		return false, fmt.Errorf("store: list status history for reopened posting: %w", err)
	}
	if len(history) < 2 {
		return false, nil
	}
	application, err := applicationFromRow(appRow)
	if err != nil {
		return false, err
	}
	last, err := parseStatusChange(history[len(history)-1])
	if err != nil {
		return false, err
	}
	if application.Status != ApplicationStatusPostingClosed ||
		last.Status != ApplicationStatusPostingClosed ||
		last.ChangedBy != StatusChangedBySync {
		return false, nil
	}
	previous, err := parseStatusChange(history[len(history)-2])
	if err != nil {
		return false, err
	}
	restore := previous.Status
	if _, err := qtx.UpdateApplicationStatus(ctx, db.UpdateApplicationStatusParams{
		PostingID: postingID,
		Status:    sql.NullString{String: restore.String(), Valid: true},
	}); err != nil {
		return false, fmt.Errorf("store: restore application for reopened posting: %w", err)
	}
	if err := recordApplicationStatus(ctx, qtx, application.ID, restore, StatusChangedBySync); err != nil {
		return false, err
	}
	return true, nil
}

// MarkPostingReopened marks a previously-closed posting open again, e.g.
// because it reappeared in a fetch.
func (s *Store) MarkPostingReopened(ctx context.Context, id int64) error {
	return s.queries.MarkPostingReopened(ctx, id)
}

// ListDistinctDepartmentsForCompany returns the distinct, non-empty
// department values seen across a company's ingested postings -- the
// keyspace to offer when picking department filter values, since
// department is a company-specific vocabulary, not a fixed enum.
func (s *Store) ListDistinctDepartmentsForCompany(ctx context.Context, companyID int64) ([]string, error) {
	return s.queries.ListDistinctDepartmentsForCompany(ctx, companyID)
}

// ListDistinctLocationsForCompany is ListDistinctDepartmentsForCompany
// for location values.
func (s *Store) ListDistinctLocationsForCompany(ctx context.Context, companyID int64) ([]string, error) {
	return s.queries.ListDistinctLocationsForCompany(ctx, companyID)
}

// CountOpenPostingsByCompany returns, per company id, how many postings are
// still open on the job board and not archived: what the company's posting
// list shows by default. Companies with none are absent from the map.
func (s *Store) CountOpenPostingsByCompany(ctx context.Context) (map[int64]int, error) {
	rows, err := s.queries.CountOpenPostingsByCompany(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int, len(rows))
	for _, r := range rows {
		counts[r.CompanyID] = int(r.OpenPostings)
	}
	return counts, nil
}
