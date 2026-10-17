package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/store"
)

type scoreViewer interface {
	Pending(ctx context.Context, r store.Runner, rescoreProfileSHA256 string, beforeID int64, limit int) (*score.PendingPostingsPage, error)
	HasRepeat(ctx context.Context, r store.Runner, postingID int64, contentSHA256, profileSHA256, model string) (bool, error)
	ListByPosting(ctx context.Context, r store.Runner, postingID int64) ([]score.Score, error)
}

type scoreRecorder interface {
	Insert(ctx context.Context, r store.Runner, provenance score.Provenance, entry score.Entry) error
}

type scoringPostingViewer interface {
	ContentSHA256(ctx context.Context, r store.Runner, id int64) (string, error)
}

type ScoringService struct {
	db            store.Store
	scoreViewer   scoreViewer
	scoreRecorder scoreRecorder
	postingViewer scoringPostingViewer
}

func NewScoringService(db store.Store) *ScoringService {
	scores := &repository.ScoreRepository{}
	return &ScoringService{
		db:            db,
		scoreViewer:   scores,
		scoreRecorder: scores,
		postingViewer: &repository.PostingRepository{},
	}
}

// Pending returns one page of the postings with no score on their current
// content, newest first, before beforeID (0 for the first page). For a
// rescore, rescoreProfileSHA256 names the profile; "" is no rescore.
func (service *ScoringService) Pending(ctx context.Context, rescoreProfileSHA256 string, beforeID int64, limit int) (*score.PendingPostingsPage, error) {
	return service.scoreViewer.Pending(ctx, service.db, rescoreProfileSHA256, beforeID, limit)
}

// ForPosting returns every score postingID was given, newest, its current
// one, first.
func (service *ScoringService) ForPosting(ctx context.Context, postingID int64) ([]score.Score, error) {
	return service.scoreViewer.ListByPosting(ctx, service.db, postingID)
}

// Record records entries as scores from provenance, all together or not
// at all: a failing write undoes the whole batch. An entry that can't be
// recorded is refused, and the result says why. A call with no model or
// profile hash records nothing.
func (service *ScoringService) Record(ctx context.Context, provenance score.Provenance, entries []score.Entry) (*score.RecordResult, error) {
	switch {
	case provenance.Model == "":
		return nil, errors.New("scoring: record scores: model is required")
	case provenance.ProfileSHA256 == "":
		return nil, errors.New("scoring: record scores: profile hash is required")
	}
	var result score.RecordResult
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		for _, e := range entries {
			why, ok, err := service.check(ctx, tx, provenance, e)
			if err != nil {
				return err
			}
			if !ok {
				result.Refused = append(result.Refused, score.Refused{PostingID: e.PostingID, Why: why})
				continue
			}
			if err := service.scoreRecorder.Insert(ctx, tx, provenance, e); err != nil {
				return err
			}
			result.Recorded = append(result.Recorded, e.PostingID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// check says whether e can be recorded, and if not, why. It reads inside
// the transaction, so an entry earlier in the same batch counts.
func (service *ScoringService) check(ctx context.Context, tx store.Tx, provenance score.Provenance, e score.Entry) (score.Refusal, bool, error) {
	switch {
	case e.Score < score.Min || e.Score > score.Max:
		return score.OutOfRange, false, nil
	case e.Reason == "":
		return score.NoReason, false, nil
	}
	current, err := service.postingViewer.ContentSHA256(ctx, tx, e.PostingID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return score.NoSuchPosting, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("scoring: posting %d: %w", e.PostingID, err)
	case current != e.ContentSHA256:
		return score.ContentChanged, false, nil
	}
	repeat, err := service.scoreViewer.HasRepeat(ctx, tx, e.PostingID, e.ContentSHA256, provenance.ProfileSHA256, provenance.Model)
	if err != nil {
		return 0, false, err
	}
	if repeat {
		return score.Repeat, false, nil
	}
	return 0, true, nil
}
