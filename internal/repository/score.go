package repository

import (
	"context"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

type ScoreRepository struct{}

// Pending returns one page of the postings with no score on their current
// content, newest first, before beforeID (0 for the first page). For a
// rescore, rescoreProfileSHA256 names the profile, and a score against
// another one doesn't count; "" is no rescore. Eligible postings are open,
// not archived, not interested, without an application, at a company the
// user hasn't deleted, and let through by its filters.
func (repository *ScoreRepository) Pending(ctx context.Context, r store.Runner, rescoreProfileSHA256 string, beforeID int64, limit int) (*domain.PendingPostingsPage, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("scoring: pending postings: limit %d, want at least 1", limit)
	}
	params := sqlcdb.ListPendingPostingsParams{
		// One more than asked for, to learn whether another page follows.
		MaxRows: int64(limit) + 1,
	}
	if rescoreProfileSHA256 != "" {
		params.RescoreProfile = rescoreProfileSHA256
	}
	if beforeID != 0 {
		params.BeforeID = beforeID
	}
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.ListPendingPostingsRow, error) {
		return q.ListPendingPostings(ctx, params)
	})
	if err != nil {
		return nil, fmt.Errorf("scoring: pending postings: %w", err)
	}
	page := domain.PendingPostingsPage{}
	if len(rows) > limit {
		page.HasMore = true
		rows = rows[:limit]
	}
	page.Postings = make([]domain.PendingPosting, len(rows))
	for i, row := range rows {
		page.Total = int(row.Total)
		reason := domain.PendingNew
		switch {
		case row.HasCurrentScore:
			reason = domain.PendingRescore
		case row.HasScore:
			reason = domain.PendingChanged
		}
		page.Postings[i] = domain.PendingPosting{
			ID:            row.ID,
			Title:         row.Title,
			CompanyName:   row.CompanyName,
			Department:    row.Department,
			Location:      row.Location,
			WorkplaceType: row.WorkplaceType,
			PublishedAt:   row.PublishedAt.Time,
			ContentSHA256: row.ContentSha256,
			Reason:        reason,
		}
	}
	return &page, nil
}

// Insert records entry as a score from provenance.
func (repository *ScoreRepository) Insert(ctx context.Context, r store.Runner, provenance domain.Provenance, entry domain.Entry) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.CreatePostingScore(ctx, sqlcdb.CreatePostingScoreParams{
			PostingID:     entry.PostingID,
			Score:         int64(entry.Score),
			Reason:        entry.Reason,
			ContentSha256: entry.ContentSHA256,
			ProfileSha256: provenance.ProfileSHA256,
			Model:         provenance.Model,
			ClientName:    provenance.ClientName,
			ClientVersion: provenance.ClientVersion,
		})
	})
	if err != nil {
		return fmt.Errorf("scoring: record score for posting %d: %w", entry.PostingID, err)
	}
	return nil
}

// HasRepeat reports whether postingID already has a score on
// contentSHA256, against profileSHA256, from model.
func (repository *ScoreRepository) HasRepeat(ctx context.Context, r store.Runner, postingID int64, contentSHA256, profileSHA256, model string) (bool, error) {
	repeat, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (bool, error) {
		return q.HasRepeatScore(ctx, sqlcdb.HasRepeatScoreParams{
			PostingID:     postingID,
			ContentSha256: contentSHA256,
			ProfileSha256: profileSHA256,
			Model:         model,
		})
	})
	if err != nil {
		return false, fmt.Errorf("scoring: posting %d: %w", postingID, err)
	}
	return repeat, nil
}

// ListByPosting returns every score postingID was given, newest, its
// current one, first.
func (repository *ScoreRepository) ListByPosting(ctx context.Context, r store.Runner, postingID int64) ([]domain.Score, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.PostingScore, error) {
		return q.ListPostingScores(ctx, postingID)
	})
	if err != nil {
		return nil, fmt.Errorf("scoring: scores of posting %d: %w", postingID, err)
	}
	return fromScoreRows(rows), nil
}

// GetMany returns the scores with the given ids that exist, in no order.
func (repository *ScoreRepository) GetMany(ctx context.Context, r store.Runner, ids []int64) ([]domain.Score, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.PostingScore, error) {
		return q.ListPostingScoresByID(ctx, ids)
	})
	if err != nil {
		return nil, fmt.Errorf("scoring: scores by id: %w", err)
	}
	return fromScoreRows(rows), nil
}

func fromScoreRows(rows []sqlcdb.PostingScore) []domain.Score {
	scores := make([]domain.Score, len(rows))
	for i, row := range rows {
		scores[i] = domain.Score{
			ID:        row.ID,
			PostingID: row.PostingID,
			Provenance: domain.Provenance{
				Model:         row.Model,
				ClientName:    row.ClientName,
				ClientVersion: row.ClientVersion,
				ProfileSHA256: row.ProfileSha256,
			},
			Score:         int(row.Score),
			Reason:        row.Reason,
			ContentSHA256: row.ContentSha256,
			ScoredAt:      row.ScoredAt,
		}
	}
	return scores
}
