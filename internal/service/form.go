package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

type formViewer interface {
	Get(ctx context.Context, r store.Runner, postingID int64) (*posting.Form, error)
}

type formWriter interface {
	Save(ctx context.Context, r store.Runner, postingID int64, formJSON string) error
}

// FormService holds what each posting's application form asks for.
type FormService struct {
	db         store.Store
	formViewer formViewer
	formWriter formWriter
}

func NewFormService(db store.Store) *FormService {
	forms := &repository.PostingForms{}
	return &FormService{db: db, formViewer: forms, formWriter: forms}
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
