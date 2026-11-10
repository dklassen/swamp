package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// FormConfig bounds each form fetch by FetchTimeout.
type FormConfig struct {
	FetchTimeout time.Duration
}

// FormRead is a posting's current application form. A failed fetch isn't
// an error, since drafting goes ahead without the form: FetchError says
// what failed.
type FormRead struct {
	Form       *jobboard.ApplicationForm
	FetchError string
}

// currentForm returns the posting's stored application form, fetching it
// from the company's board if there's none yet. A source with no fetcher
// has only a form saved by hand.
func (service *DraftingService) currentForm(ctx context.Context, p posting.Posting, c company.Company) (*FormRead, error) {
	stored, err := service.formViewer.Get(ctx, service.db, p.ID)
	if err == nil {
		form, err := decodeForm(stored)
		if err != nil {
			return nil, err
		}
		return &FormRead{Form: form}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	fetcher, ok := service.formFetchers[c.Source]
	if !ok {
		return &FormRead{}, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, service.formConfig.FetchTimeout)
	defer cancel()
	fetched, err := fetcher.FetchApplicationForm(fetchCtx, c.SourceRef, p.SourceID)
	if err != nil {
		return &FormRead{FetchError: err.Error()}, nil
	}
	encoded, err := json.Marshal(fetched)
	if err != nil {
		return nil, fmt.Errorf("encode application form: %w", err)
	}
	if err := service.formWriter.Save(ctx, service.db, p.ID, string(encoded)); err != nil {
		return nil, err
	}
	return &FormRead{Form: &fetched}, nil
}
