package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// IngestedFields is a posting's content as ingested from its source --
// exactly the fields store.Posting and CreatePostingParams share, and
// exactly the fields a re-fetch needs to compare to detect a real
// content change (see sync/company.go's use of this type). Pulled into
// one type so there's a single place defining "what counts as a
// posting's content," rather than that field list being hand-copied at
// every site that needs it (see decisions.log, #57).
//
// json tags pin the field names exactly as they already serialize today
// (Go's default reflect-based names) rather than changing them -- the
// point is making a future Go-side rename require an explicit tag edit
// to also change the JSON contract an external LLM agent reads
// (.agents/skills/apply-to-posting/SKILL.md), not changing that contract
// now (see decisions.log, #59).
type IngestedFields struct {
	Title           string    `json:"Title"`
	Department      string    `json:"Department"`
	Team            string    `json:"Team"`
	Location        string    `json:"Location"`
	EmploymentType  string    `json:"EmploymentType"`
	WorkplaceType   string    `json:"WorkplaceType"`
	DescriptionHTML string    `json:"DescriptionHTML"`
	DescriptionText string    `json:"DescriptionText"`
	JobURL          string    `json:"JobURL"`
	ApplicationURL  string    `json:"ApplicationURL"`
	PublishedAt     time.Time `json:"PublishedAt"`
	RawPayload      string    `json:"RawPayload"`
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
			PublishedAt:     row.PublishedAt.Time,
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
func nullPublishedAt(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// UpsertPosting inserts a new posting or updates the existing one for the
// same (source, source_id), keeping the row's listing_status untouched on
// update (see UpdatePosting query comment for why).
//
// On insert, it also creates the posting's markup row, defaulted to
// user status new with empty notes, in the same transaction, so every
// posting always has exactly one markup row and callers never have to
// remember a second call to create it. This is deliberately not exposed
// as two separate Store calls.
func (s *Store) UpsertPosting(ctx context.Context, params CreatePostingParams) (Posting, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return Posting{}, fmt.Errorf("store: begin upsert posting tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.queries.WithTx(tx)

	existing, err := qtx.GetPostingBySourceAndSourceID(ctx, db.GetPostingBySourceAndSourceIDParams{
		Source:   params.Source,
		SourceID: params.SourceID,
	})

	var row db.Posting
	switch {
	case errors.Is(err, sql.ErrNoRows):
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
			return Posting{}, fmt.Errorf("store: create posting: %w", err)
		}
		if _, err := qtx.CreatePostingMarkup(ctx, row.ID); err != nil {
			return Posting{}, fmt.Errorf("store: create posting markup: %w", err)
		}
	case err != nil:
		return Posting{}, fmt.Errorf("store: get posting by source: %w", err)
	default:
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
			return Posting{}, fmt.Errorf("store: update posting: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Posting{}, fmt.Errorf("store: commit upsert posting tx: %w", err)
	}
	return postingFromRow(row), nil
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
