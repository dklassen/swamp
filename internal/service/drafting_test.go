package service_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/store"
)

func TestDraftingService_Prepare_FirstCall_StartsTheApplicationAndItsFolder(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")

	got, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got.Application.PostingID != p.ID || got.Application.Status != application.Started {
		t.Errorf("Application = %+v, want a started application for posting %d", got.Application, p.ID)
	}
	stored, err := s.Applications.Get(t.Context(), got.Application.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(*stored, got.Application); diff != "" {
		t.Errorf("Application against the stored one (-stored +got):\n%s", diff)
	}
	for _, documentType := range document.Types() {
		dir := filepath.Dir(got.Documents[documentType].Path)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("%s's folder %q: stat err = %v, want a directory", documentType, dir, err)
		}
	}
}

func TestDraftingService_Prepare_SecondCall_ResumesTheSameApplication(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	first, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare (first): %v", err)
	}

	second, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare (second): %v", err)
	}
	if diff := cmp.Diff(first.Application, second.Application); diff != "" {
		t.Errorf("second call's application (-first +second):\n%s", diff)
	}
	if diff := cmp.Diff(first.Documents, second.Documents); diff != "" {
		t.Errorf("second call's documents (-first +second):\n%s", diff)
	}
}

func TestDraftingService_Prepare_ClosedPosting_IsRefused(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	if err := s.Postings.Close(t.Context(), p.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := s.Drafting.Prepare(t.Context(), p.ID)
	if !errors.Is(err, service.ErrPostingClosed) {
		t.Fatalf("Prepare = %+v, %v; want ErrPostingClosed", got, err)
	}
	if _, err := s.Postings.Application(t.Context(), p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Application after a refused Prepare: err = %v, want store.ErrNotFound", err)
	}
}

func TestDraftingService_Prepare_PostingAtADeletedCompany_IsRefused(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	acme := testutil.MustAddCompany(t, s, "Acme")
	p := testutil.MustIngestPostingFor(t, s, acme.ID, "job-1", "Engineer")
	if _, err := s.Companies.Delete(t.Context(), acme.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := s.Drafting.Prepare(t.Context(), p.ID)
	var deleted *service.CompanyDeletedError
	if !errors.As(err, &deleted) {
		t.Fatalf("Prepare = %+v, %v; want a CompanyDeletedError", got, err)
	}
	if want := (service.CompanyDeletedError{PostingID: p.ID, CompanyName: "Acme"}); *deleted != want {
		t.Errorf("CompanyDeletedError = %+v, want %+v", *deleted, want)
	}
}
