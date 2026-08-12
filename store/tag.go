package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// Tag is a user-defined label, reusable across postings. Soft-deleted via
// DeletedAt so existing posting associations survive a tag "deletion"
// instead of being cascade-removed.
type Tag struct {
	ID        int64
	Name      string
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func tagFromRow(row db.Tag) Tag {
	t := Tag{
		ID:        row.ID,
		Name:      row.Name,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.DeletedAt.Valid {
		t.DeletedAt = &row.DeletedAt.Time
	}
	return t
}

func (s *Store) CreateTag(ctx context.Context, name string) (Tag, error) {
	row, err := s.queries.CreateTag(ctx, name)
	if err != nil {
		return Tag{}, err
	}
	return tagFromRow(row), nil
}

func (s *Store) GetTagByName(ctx context.Context, name string) (Tag, error) {
	row, err := s.queries.GetTagByName(ctx, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Tag{}, ErrNotFound
		}
		return Tag{}, err
	}
	return tagFromRow(row), nil
}

// ListTags returns active (non-soft-deleted) tags, the pick list for
// tagging postings.
func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.queries.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	tags := make([]Tag, len(rows))
	for i, row := range rows {
		tags[i] = tagFromRow(row)
	}
	return tags, nil
}

func (s *Store) SoftDeleteTag(ctx context.Context, id int64) error {
	return s.queries.SoftDeleteTag(ctx, id)
}

func (s *Store) AddTagToPosting(ctx context.Context, postingID, tagID int64) error {
	return s.queries.AddTagToPosting(ctx, db.AddTagToPostingParams{
		PostingID: postingID,
		TagID:     tagID,
	})
}

func (s *Store) RemoveTagFromPosting(ctx context.Context, postingID, tagID int64) error {
	return s.queries.RemoveTagFromPosting(ctx, db.RemoveTagFromPostingParams{
		PostingID: postingID,
		TagID:     tagID,
	})
}

// ListTagsForPosting returns every tag associated with a posting,
// including soft-deleted ones: the association is the historical record
// of what a posting was tagged with, and shouldn't disappear just because
// the tag was later retired.
func (s *Store) ListTagsForPosting(ctx context.Context, postingID int64) ([]Tag, error) {
	rows, err := s.queries.ListTagsForPosting(ctx, postingID)
	if err != nil {
		return nil, err
	}
	tags := make([]Tag, len(rows))
	for i, row := range rows {
		tags[i] = tagFromRow(row)
	}
	return tags, nil
}
