package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/store"
)

type scoreViewer interface {
	Pending(ctx context.Context, r store.Runner, rescoreProfileSHA256 string, beforeID int64, limit int) (*score.PendingPostingsPage, error)
	HasRepeat(ctx context.Context, r store.Runner, postingID int64, contentSHA256, profileSHA256, model string) (bool, error)
	ListByPosting(ctx context.Context, r store.Runner, postingID int64) ([]score.Score, error)
	GetMany(ctx context.Context, r store.Runner, ids []int64) ([]score.Score, error)
}

type scoreRecorder interface {
	Insert(ctx context.Context, r store.Runner, provenance score.Provenance, entry score.Entry) error
}

type scoringPostingViewer interface {
	ContentSHA256(ctx context.Context, r store.Runner, id int64) (string, error)
}

type ScoringService struct {
	db            store.Store
	documents     *documents.Store
	scoreViewer   scoreViewer
	scoreRecorder scoreRecorder
	postingViewer scoringPostingViewer
}

func NewScoringService(db store.Store, docs *documents.Store) *ScoringService {
	scores := &repository.ScoreRepository{}
	return &ScoringService{
		db:            db,
		documents:     docs,
		scoreViewer:   scores,
		scoreRecorder: scores,
		postingViewer: &repository.PostingRepository{},
	}
}

// Pending returns one page of the postings to score against the profile
// with profileSHA256, which must be the profile now, newest first, before
// beforeID (0 for the first page): those with no score on their current
// content, or for a rescore, none against this profile.
func (service *ScoringService) Pending(ctx context.Context, profileSHA256 string, rescore bool, beforeID int64, limit int) (*score.PendingPostingsPage, error) {
	if err := service.requireProfile(profileSHA256); err != nil {
		return nil, err
	}
	var rescoreProfileSHA256 string
	if rescore {
		rescoreProfileSHA256 = profileSHA256
	}
	return service.scoreViewer.Pending(ctx, service.db, rescoreProfileSHA256, beforeID, limit)
}

// ForPosting returns every score postingID was given, newest, its current
// one, first.
func (service *ScoringService) ForPosting(ctx context.Context, postingID int64) ([]score.Score, error) {
	return service.scoreViewer.ListByPosting(ctx, service.db, postingID)
}

// GetMany returns the scores with the given ids, in no order. An id with
// no score is left out.
func (service *ScoringService) GetMany(ctx context.Context, ids []int64) ([]score.Score, error) {
	return service.scoreViewer.GetMany(ctx, service.db, ids)
}

// Record records entries as scores from provenance, all together or not
// at all: a failing write undoes the whole batch. An entry that can't be
// recorded is refused, and the result says why. A call with no model, or
// whose profile hash isn't the profile's now, records nothing.
func (service *ScoringService) Record(ctx context.Context, provenance score.Provenance, entries []score.Entry) (*score.RecordResult, error) {
	switch {
	case provenance.Model == "":
		return nil, errors.New("scoring: record scores: model is required")
	case provenance.ProfileSHA256 == "":
		return nil, errors.New("scoring: record scores: profile hash is required")
	}
	if err := service.requireProfile(provenance.ProfileSHA256); err != nil {
		return nil, err
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

// ErrProfileChanged is the error for a profile hash that isn't the
// profile's now: it changed since, or was never read.
var ErrProfileChanged = errors.New("scoring: the profile isn't the version given")

// NoProfileError is the error for scoring with no profile at Path.
type NoProfileError struct {
	Path string
}

func (e *NoProfileError) Error() string {
	return fmt.Sprintf("scoring: there's no profile at %s to score against", e.Path)
}

// requireProfile returns why scores can't be judged against the profile
// with profileSHA256, or nil when it's the profile now.
func (service *ScoringService) requireProfile(profileSHA256 string) error {
	content, err := service.documents.ReadProfile()
	if errors.Is(err, fs.ErrNotExist) {
		return &NoProfileError{Path: service.documents.ProfilePath()}
	}
	if err != nil {
		return fmt.Errorf("scoring: read profile: %w", err)
	}
	if document.ContentSHA256(content) != profileSHA256 {
		return ErrProfileChanged
	}
	return nil
}
