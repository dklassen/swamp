package service

import (
	"context"
	"errors"
	"time"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// FormFetcher reads a posting's application form from its board.
type FormFetcher interface {
	FetchApplicationForm(ctx context.Context, boardToken, jobID string) (jobboard.ApplicationForm, error)
}

// FormConfig bounds each form fetch by FetchTimeout, and decides with Now
// whether a stored form is stale.
type FormConfig struct {
	FetchTimeout time.Duration
	Now          func() time.Time
}

// formMaxAge is how long a stored form is reused before it's fetched again:
// forms rarely change once a posting is up, and a stale one only costs a
// refetch.
const formMaxAge = 7 * 24 * time.Hour

// FormRead is a posting's current application form. A failed fetch isn't
// an error, since drafting goes ahead without the form: FetchError says
// what failed, and Form is the stale stored form, or nil if none was.
type FormRead struct {
	Form       *jobboard.ApplicationForm
	FetchError string
}

// CurrentForm returns the posting's application form, fetching it from
// the company's board when the stored one is older than formMaxAge. A
// source with no fetcher has only the form saved by hand, however old.
func (service *PostingService) CurrentForm(ctx context.Context, p posting.Posting, c company.Company) (*FormRead, error) {
	stored, err := service.formViewer.Get(ctx, service.db, p.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	var storedForm *jobboard.ApplicationForm
	if stored != nil {
		if storedForm, err = decodeForm(stored); err != nil {
			return nil, err
		}
	}
	fetcher, ok := service.formFetchers[c.Source]
	if !ok || stored != nil && service.formConfig.Now().Sub(stored.FetchedAt) < formMaxAge {
		return &FormRead{Form: storedForm}, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, service.formConfig.FetchTimeout)
	defer cancel()
	fetched, err := fetcher.FetchApplicationForm(fetchCtx, c.SourceRef, p.SourceID)
	if err != nil {
		return &FormRead{Form: storedForm, FetchError: err.Error()}, nil
	}
	if err := service.SaveForm(ctx, p.ID, fetched); err != nil {
		return nil, err
	}
	return &FormRead{Form: &fetched}, nil
}
