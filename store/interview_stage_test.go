package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestCreateInterviewStage_ThenList_ReturnsCreatedStage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	created, err := s.CreateInterviewStage(ctx, application.ID, 1, "Recruiter Screen", nil, "")
	if err != nil {
		t.Fatalf("CreateInterviewStage: %v", err)
	}

	got, err := s.ListInterviewStages(ctx, application.ID)
	if err != nil {
		t.Fatalf("ListInterviewStages: %v", err)
	}

	want := []InterviewStage{created}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListInterviewStages mismatch (-want +got):\n%s", diff)
	}
}

func TestListInterviewStages_ReturnsStagesInSequenceOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	onsite, err := s.CreateInterviewStage(ctx, application.ID, 2, "Onsite", nil, "")
	if err != nil {
		t.Fatalf("CreateInterviewStage: %v", err)
	}
	screen, err := s.CreateInterviewStage(ctx, application.ID, 1, "Recruiter Screen", nil, "")
	if err != nil {
		t.Fatalf("CreateInterviewStage: %v", err)
	}

	got, err := s.ListInterviewStages(ctx, application.ID)
	if err != nil {
		t.Fatalf("ListInterviewStages: %v", err)
	}

	want := []InterviewStage{screen, onsite}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListInterviewStages mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateInterviewStageOutcome_UpdatesOutcome(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	stage, err := s.CreateInterviewStage(ctx, application.ID, 1, "Recruiter Screen", nil, "")
	if err != nil {
		t.Fatalf("CreateInterviewStage: %v", err)
	}

	updated, err := s.UpdateInterviewStageOutcome(ctx, stage.ID, "passed")
	if err != nil {
		t.Fatalf("UpdateInterviewStageOutcome: %v", err)
	}
	if updated.Outcome != "passed" {
		t.Fatalf("Outcome = %q, want %q", updated.Outcome, "passed")
	}
}

func TestUpdateInterviewStageOutcome_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpdateInterviewStageOutcome(ctx, 999, "passed")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateInterviewStageOutcome error = %v, want ErrNotFound", err)
	}
}

func TestUpdateInterviewStage_ReplacesEditableFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	stage, err := s.CreateInterviewStage(ctx, application.ID, 1, "Recruiter Screen", nil, "")
	if err != nil {
		t.Fatalf("CreateInterviewStage: %v", err)
	}

	date := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	updated, err := s.UpdateInterviewStage(ctx, stage.ID, 2, "Technical Screen", &date, "passed", "Went well")
	if err != nil {
		t.Fatalf("UpdateInterviewStage: %v", err)
	}

	want := InterviewStage{
		ID:            stage.ID,
		ApplicationID: application.ID,
		Sequence:      2,
		Name:          "Technical Screen",
		StageDate:     &date,
		Outcome:       "passed",
		Notes:         "Went well",
		CreatedAt:     stage.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
	}
	if diff := cmp.Diff(want, updated); diff != "" {
		t.Fatalf("UpdateInterviewStage mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateInterviewStage_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpdateInterviewStage(ctx, 999, 1, "Recruiter Screen", nil, "pending", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateInterviewStage error = %v, want ErrNotFound", err)
	}
}

func TestDeleteInterviewStage_RemovesStage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	stage, err := s.CreateInterviewStage(ctx, application.ID, 1, "Recruiter Screen", nil, "")
	if err != nil {
		t.Fatalf("CreateInterviewStage: %v", err)
	}

	if err := s.DeleteInterviewStage(ctx, stage.ID); err != nil {
		t.Fatalf("DeleteInterviewStage: %v", err)
	}

	got, err := s.ListInterviewStages(ctx, application.ID)
	if err != nil {
		t.Fatalf("ListInterviewStages: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListInterviewStages after delete = %+v, want empty", got)
	}
}
