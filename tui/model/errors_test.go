package model_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

func TestErrorOf(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("loading company 7: %w", store.ErrNotFound)
	unmapped := errors.New("company name already taken")
	tests := []struct {
		name string
		err  error
		want message.Error
	}{
		{"not found", store.ErrNotFound, message.Error{Code: message.ErrNotFound, Text: store.ErrNotFound.Error()}},
		{"sync in progress", sync.ErrSyncInProgress, message.Error{Code: message.ErrSyncInProgress, Text: sync.ErrSyncInProgress.Error()}},
		{"posting closed", service.ErrPostingClosed, message.Error{Code: message.ErrPostingClosed, Text: service.ErrPostingClosed.Error()}},
		{"wrapped", wrapped, message.Error{Code: message.ErrNotFound, Text: wrapped.Error()}},
		{"unmapped", unmapped, message.Error{Code: message.ErrOther, Text: unmapped.Error()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, model.ErrorOf(tt.err)); diff != "" {
				t.Errorf("ErrorOf(%v) (-want +got):\n%s", tt.err, diff)
			}
		})
	}
}
