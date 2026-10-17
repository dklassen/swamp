package repository

import (
	"context"
	"errors"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// FromHistoryRow converts a status history row, failing on a status or
// changed_by value it doesn't know.
func FromHistoryRow(row sqlcdb.ApplicationStatusHistory) (domain.StatusChange, error) {
	status, err := domain.ParseStatus(row.Status)
	if err != nil {
		return domain.StatusChange{}, err
	}
	changedBy, err := domain.ParseChangedBy(row.ChangedBy)
	if err != nil {
		return domain.StatusChange{}, err
	}
	return domain.StatusChange{
		ID:            row.ID,
		ApplicationID: row.ApplicationID,
		Status:        status,
		ChangedBy:     changedBy,
		ChangedAt:     row.ChangedAt,
	}, nil
}

type ApplicationHistoryRepository struct{}

// Insert records that applicationID entered status, made by changedBy.
func (repository *ApplicationHistoryRepository) Insert(ctx context.Context, r store.Runner, applicationID int64, status domain.Status, changedBy domain.ChangedBy) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.CreateApplicationStatusHistory(ctx, sqlcdb.CreateApplicationStatusHistoryParams{
			ApplicationID: applicationID,
			Status:        status.String(),
			ChangedBy:     changedBy.String(),
		})
	})
	if err != nil {
		return fmt.Errorf("record application %d status %s: %w", applicationID, status, err)
	}
	return nil
}

// List returns an application's status changes, oldest first.
func (repository *ApplicationHistoryRepository) List(ctx context.Context, r store.Runner, applicationID int64) ([]domain.StatusChange, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.ApplicationStatusHistory, error) {
		return q.ListApplicationStatusHistory(ctx, applicationID)
	})
	if err != nil {
		return nil, fmt.Errorf("application: status history: %w", err)
	}
	history := make([]domain.StatusChange, len(rows))
	for i, row := range rows {
		if history[i], err = FromHistoryRow(row); err != nil {
			return nil, err
		}
	}
	return history, nil
}

// Latest returns an application's newest status change, or
// store.ErrNotFound when it has none.
func (repository *ApplicationHistoryRepository) Latest(ctx context.Context, r store.Runner, applicationID int64) (*domain.StatusChange, error) {
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.ApplicationStatusHistory, error) {
		return q.LatestApplicationStatusChange(ctx, applicationID)
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("application: latest status change for application %d: %w", applicationID, err)
	}
	change, err := FromHistoryRow(row)
	if err != nil {
		return nil, fmt.Errorf("application: latest status change for application %d: %w", applicationID, err)
	}
	return &change, nil
}
