package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// InterviewStage is one round in a posting's interview process (e.g.
// recruiter screen, technical, onsite), owned by the user and ordered by
// Sequence within a posting.
type InterviewStage struct {
	ID        int64
	PostingID int64
	Sequence  int64
	Name      string
	StageDate *time.Time
	Outcome   string
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func interviewStageFromRow(row db.InterviewStage) InterviewStage {
	s := InterviewStage{
		ID:        row.ID,
		PostingID: row.PostingID,
		Sequence:  row.Sequence,
		Name:      row.Name,
		Outcome:   row.Outcome,
		Notes:     row.Notes,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.StageDate.Valid {
		s.StageDate = &row.StageDate.Time
	}
	return s
}

func (s *Store) CreateInterviewStage(ctx context.Context, postingID int64, sequence int64, name string, stageDate *time.Time, notes string) (InterviewStage, error) {
	row, err := s.queries.CreateInterviewStage(ctx, db.CreateInterviewStageParams{
		PostingID: postingID,
		Sequence:  sequence,
		Name:      name,
		StageDate: nullTime(stageDate),
		Notes:     notes,
	})
	if err != nil {
		return InterviewStage{}, err
	}
	return interviewStageFromRow(row), nil
}

func (s *Store) ListInterviewStages(ctx context.Context, postingID int64) ([]InterviewStage, error) {
	rows, err := s.queries.ListInterviewStagesByPosting(ctx, postingID)
	if err != nil {
		return nil, err
	}
	stages := make([]InterviewStage, len(rows))
	for i, row := range rows {
		stages[i] = interviewStageFromRow(row)
	}
	return stages, nil
}

func (s *Store) UpdateInterviewStageOutcome(ctx context.Context, id int64, outcome string) (InterviewStage, error) {
	row, err := s.queries.UpdateInterviewStageOutcome(ctx, db.UpdateInterviewStageOutcomeParams{
		ID:      id,
		Outcome: outcome,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return InterviewStage{}, ErrNotFound
		}
		return InterviewStage{}, err
	}
	return interviewStageFromRow(row), nil
}

// UpdateInterviewStage replaces every editable field on a stage (a full
// edit, as opposed to UpdateInterviewStageOutcome's single-field update).
func (s *Store) UpdateInterviewStage(ctx context.Context, id int64, sequence int64, name string, stageDate *time.Time, outcome, notes string) (InterviewStage, error) {
	row, err := s.queries.UpdateInterviewStage(ctx, db.UpdateInterviewStageParams{
		ID:        id,
		Sequence:  sequence,
		Name:      name,
		StageDate: nullTime(stageDate),
		Outcome:   outcome,
		Notes:     notes,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return InterviewStage{}, ErrNotFound
		}
		return InterviewStage{}, err
	}
	return interviewStageFromRow(row), nil
}

func (s *Store) DeleteInterviewStage(ctx context.Context, id int64) error {
	return s.queries.DeleteInterviewStage(ctx, id)
}
