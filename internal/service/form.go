package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/repository"
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

type formViewer interface {
	Get(ctx context.Context, r store.Runner, postingID int64) (*posting.Form, error)
}

type formWriter interface {
	Save(ctx context.Context, r store.Runner, postingID int64, formJSON string) error
	Insert(ctx context.Context, r store.Runner, postingID int64, formJSON string) error
}

type missingFormLister interface {
	ListOpenWithout(ctx context.Context, r store.Runner, companyID int64) ([]posting.Posting, error)
}

// FormService holds what each posting's application form asks for, and
// fetches it from the boards in formFetchers, keyed by source.
type FormService struct {
	db           store.Store
	formViewer   formViewer
	formWriter   formWriter
	missingForms missingFormLister
	formFetchers map[string]FormFetcher
	formConfig   FormConfig
}

func NewFormService(db store.Store, formFetchers map[string]FormFetcher, formConfig FormConfig) *FormService {
	forms := &repository.PostingForms{}
	return &FormService{
		db:           db,
		formViewer:   forms,
		formWriter:   forms,
		missingForms: forms,
		formFetchers: formFetchers,
		formConfig:   formConfig,
	}
}

// FormFetch counts one FetchMissing's forms.
type FormFetch struct {
	Fetched int
	Failed  int
}

// FetchMissing fetches and stores the form of each of c's open postings
// that has none. A failed fetch is counted, not returned: the posting is
// still missing its form, so the next call tries it again.
func (service *FormService) FetchMissing(ctx context.Context, c company.Company) (*FormFetch, error) {
	fetcher, ok := service.formFetchers[c.Source]
	if !ok {
		return &FormFetch{}, nil
	}
	missing, err := service.missingForms.ListOpenWithout(ctx, service.db, c.ID)
	if err != nil {
		return nil, err
	}
	var (
		mu    sync.Mutex
		fetch FormFetch
		errs  []error
		wg    sync.WaitGroup
	)
	slots := make(chan struct{}, formFetchesAtOnce)
	for _, p := range missing {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			form, fetchErr := service.fetchForm(ctx, fetcher, c.SourceRef, p.SourceID)
			var storeErr error
			if fetchErr == nil {
				storeErr = service.insert(ctx, p.ID, form)
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case fetchErr != nil:
				fetch.Failed++
			case storeErr != nil:
				errs = append(errs, storeErr)
			default:
				fetch.Fetched++
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &fetch, nil
}

// formFetchesAtOnce bounds FetchMissing's requests to one board.
const formFetchesAtOnce = 4

func (service *FormService) fetchForm(ctx context.Context, fetcher FormFetcher, boardToken, jobID string) (jobboard.ApplicationForm, error) {
	ctx, cancel := context.WithTimeout(ctx, service.formConfig.FetchTimeout)
	defer cancel()
	return fetcher.FetchApplicationForm(ctx, boardToken, jobID)
}

// insert stores form unless the posting already has one, so a form saved
// by hand during the fetch stays.
func (service *FormService) insert(ctx context.Context, postingID int64, form jobboard.ApplicationForm) error {
	encoded, err := json.Marshal(form)
	if err != nil {
		return fmt.Errorf("encode application form: %w", err)
	}
	return service.formWriter.Insert(ctx, service.db, postingID, string(encoded))
}

// Save stores form as the posting's application form, replacing any
// stored one.
func (service *FormService) Save(ctx context.Context, postingID int64, form jobboard.ApplicationForm) error {
	encoded, err := json.Marshal(form)
	if err != nil {
		return fmt.Errorf("encode application form: %w", err)
	}
	return service.formWriter.Save(ctx, service.db, postingID, string(encoded))
}

// Get returns the posting's stored application form, or
// store.ErrNotFound when it has none.
func (service *FormService) Get(ctx context.Context, postingID int64) (*jobboard.ApplicationForm, error) {
	stored, err := service.formViewer.Get(ctx, service.db, postingID)
	if err != nil {
		return nil, err
	}
	return decodeForm(stored)
}

func decodeForm(stored *posting.Form) (*jobboard.ApplicationForm, error) {
	var form jobboard.ApplicationForm
	if err := json.Unmarshal([]byte(stored.JSON), &form); err != nil {
		return nil, fmt.Errorf("decode stored application form: %w", err)
	}
	return &form, nil
}
