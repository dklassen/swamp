package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// FormFetcher reads a posting's application form from its board:
// greenhouse.Client is one.
type FormFetcher interface {
	FetchApplicationForm(ctx context.Context, boardToken, jobID string) (jobboard.ApplicationForm, error)
}

// WithFormFetchers lets Prepare read application forms (#168), with a
// fetcher per company source ("greenhouse") and each fetch bounded by
// timeout (sync.Config.FetchTimeout).
func WithFormFetchers(fetchers map[string]FormFetcher, timeout time.Duration) Option {
	return func(st *Stage) {
		st.forms = fetchers
		st.fetchTimeout = timeout
	}
}

// formMaxAge is how long a stored form is reused before Prepare fetches
// it again: forms rarely change once a posting is up, and a stale one
// only costs a refetch.
const formMaxAge = 7 * 24 * time.Hour

// applicationForm is the posting's application form for Prepare: the
// stored one if it's under formMaxAge, otherwise fetched and stored. A
// fetch failure isn't an error (formErr says what happened instead),
// since drafting can go ahead without the form; a stale stored form is
// returned in that case if there is one. err is only for a broken store.
//
// A board with no fetcher (Ashby, Lever) gets the form entered by hand in
// the TUI (#184), however old, or nil if none was.
func (st *Stage) applicationForm(ctx context.Context, posting store.Posting, company store.Company) (form *jobboard.ApplicationForm, formErr string, err error) {
	stored, fetchedAt, haveStored, err := st.store.GetApplicationForm(ctx, posting.ID)
	if err != nil {
		return nil, "", fmt.Errorf("stage: %w", err)
	}
	var storedForm *jobboard.ApplicationForm
	if haveStored {
		var f jobboard.ApplicationForm
		if err := json.Unmarshal([]byte(stored), &f); err != nil {
			return nil, "", fmt.Errorf("stage: decode stored application form: %w", err)
		}
		storedForm = &f
	}

	fetcher, ok := st.forms[company.Source]
	if !ok {
		return storedForm, "", nil
	}
	if storedForm != nil && st.now().Sub(fetchedAt) < formMaxAge {
		return storedForm, "", nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, st.fetchTimeout)
	defer cancel()
	fetched, err := fetcher.FetchApplicationForm(fetchCtx, company.SourceRef, posting.SourceID)
	if err != nil {
		return storedForm, err.Error(), nil
	}
	encoded, err := json.Marshal(fetched)
	if err != nil {
		return nil, "", fmt.Errorf("stage: encode application form: %w", err)
	}
	if err := st.store.SaveApplicationForm(ctx, posting.ID, string(encoded)); err != nil {
		return nil, "", fmt.Errorf("stage: %w", err)
	}
	return &fetched, "", nil
}
