package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
)

func mustCreateApplication(t *testing.T, s *Store, postingID int64) Application {
	t.Helper()
	a, err := s.CreateApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	return a
}

func TestCreateApplication_ThenGet_ReturnsSameApplication(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	created, err := s.CreateApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if created.Status != ApplicationStatusStarted {
		t.Fatalf("Status = %s, want %s", created.Status, ApplicationStatusStarted)
	}

	got, err := s.GetApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetApplication mismatch (-created +got):\n%s", diff)
	}
}

func TestGetApplication_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetApplication(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApplication error = %v, want ErrNotFound", err)
	}
}

func TestUpdateApplicationStatus_UpdatesStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)

	updated, err := s.UpdateApplicationStatus(ctx, posting.ID, ApplicationStatusInterviewing)
	if err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}
	if updated.Status != ApplicationStatusInterviewing {
		t.Fatalf("Status = %s, want %s", updated.Status, ApplicationStatusInterviewing)
	}

	got, err := s.GetApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if diff := cmp.Diff(updated, got); diff != "" {
		t.Fatalf("GetApplication mismatch (-updated +got):\n%s", diff)
	}
}

func TestUpdateApplicationStatus_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpdateApplicationStatus(ctx, 999, ApplicationStatusInterviewing)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateApplicationStatus error = %v, want ErrNotFound", err)
	}
}

// TestGetApplication_NullStatusInDB_FailsLoudly verifies that a row with
// an actual NULL status (only reachable via something outside this
// package writing to the table directly, since store's own writes always
// supply a concrete status -- see applicationFromRow) surfaces as an
// error rather than silently coercing to some default status.
func TestGetApplication_NullStatusInDB_FailsLoudly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := s.sqlDB.ExecContext(ctx, `INSERT INTO applications (posting_id) VALUES (?)`, posting.ID); err != nil {
		t.Fatalf("insert application with NULL status: %v", err)
	}

	if _, err := s.GetApplication(ctx, posting.ID); err == nil {
		t.Fatal("GetApplication with NULL status in DB = nil error, want an error")
	}
}

func TestUpdateApplicationNotes_UpdatesNotes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)

	updated, err := s.UpdateApplicationNotes(ctx, posting.ID, "Follow up next week")
	if err != nil {
		t.Fatalf("UpdateApplicationNotes: %v", err)
	}
	if updated.Notes != "Follow up next week" {
		t.Fatalf("Notes = %q, want %q", updated.Notes, "Follow up next week")
	}
}

func TestGetApplicationByID_ReturnsTheApplication(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	created := mustCreateApplication(t, s, posting.ID)

	got, err := s.GetApplicationByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetApplicationByID: %v", err)
	}
	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetApplicationByID mismatch (-created +got):\n%s", diff)
	}
}

// TestGetApplicationByID_NonexistentID_ReturnsErrNotFound is the whole
// point of this lookup existing: callers holding only an application ID
// (`swamp export <application-id>`) need to tell an ID that names no row
// from one whose application simply has nothing drafted yet.
func TestGetApplicationByID_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetApplicationByID(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApplicationByID error = %v, want ErrNotFound", err)
	}
}

// TestCreateApplication_ClosedPosting_Refused: an application started on
// a closed posting can't be submitted, and sync never ends it, because it
// only ends applications when their posting changes to closed (#175). So
// it isn't started: nothing is written, and the error says why.
func TestCreateApplication_ClosedPosting_Refused(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}

	if _, err := s.CreateApplication(ctx, posting.ID); !errors.Is(err, ErrPostingClosed) {
		t.Fatalf("CreateApplication on a closed posting: err = %v, want ErrPostingClosed", err)
	}
	if _, err := s.GetApplication(ctx, posting.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetApplication after the refusal: err = %v, want ErrNotFound", err)
	}
}

// TestCreateApplication_NonexistentPosting_ReturnsErrNotFound: foreign
// keys aren't enforced, so without the posting check CreateApplication
// would start an application for a posting that doesn't exist (#175).
func TestCreateApplication_NonexistentPosting_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	if _, err := s.CreateApplication(context.Background(), 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateApplication(9999): err = %v, want ErrNotFound", err)
	}
}

func TestDeleteApplication_ThenGet_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	if err := s.DeleteApplication(ctx, application.ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}

	if _, err := s.GetApplication(ctx, posting.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApplication error = %v, want ErrNotFound", err)
	}
}

// A deleted application takes everything it owns with it (#232). Each
// case checks the old ID has nothing left, which is also what a new
// application would see if SQLite hands it the same rowid.
func TestDeleteApplication_RemovesWhatTheApplicationOwns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		seed  func(t *testing.T, s *Store, applicationID int64)
		count func(t *testing.T, s *Store, applicationID int64) int
	}{
		{
			name: "status history",
			seed: func(*testing.T, *Store, int64) {}, // CreateApplication records one
			count: func(t *testing.T, s *Store, applicationID int64) int {
				t.Helper()
				history, err := s.ListApplicationStatusHistory(context.Background(), applicationID)
				if err != nil {
					t.Fatalf("ListApplicationStatusHistory: %v", err)
				}
				return len(history)
			},
		},
		{
			name: "document reviews",
			seed: func(t *testing.T, s *Store, applicationID int64) {
				t.Helper()
				if _, err := s.CreateDocumentReview(context.Background(), applicationID, documents.Resume, "# Resume\n", ReviewOutcomePassed, ""); err != nil {
					t.Fatalf("CreateDocumentReview: %v", err)
				}
			},
			count: func(t *testing.T, s *Store, applicationID int64) int {
				t.Helper()
				reviews, err := s.LatestDocumentReviews(context.Background(), applicationID)
				if err != nil {
					t.Fatalf("LatestDocumentReviews: %v", err)
				}
				return len(reviews)
			},
		},
		{
			name: "document exports",
			seed: func(t *testing.T, s *Store, applicationID int64) {
				t.Helper()
				if err := s.RecordDocumentExport(context.Background(), applicationID, documents.Resume, "# Resume\n", "/exports/resume.pdf"); err != nil {
					t.Fatalf("RecordDocumentExport: %v", err)
				}
			},
			count: func(t *testing.T, s *Store, applicationID int64) int {
				t.Helper()
				exports, err := s.LatestDocumentExports(context.Background(), applicationID)
				if err != nil {
					t.Fatalf("LatestDocumentExports: %v", err)
				}
				return len(exports)
			},
		},
		{
			name: "interview stages",
			seed: func(t *testing.T, s *Store, applicationID int64) {
				t.Helper()
				if _, err := s.CreateInterviewStage(context.Background(), applicationID, 1, "Phone screen", nil, ""); err != nil {
					t.Fatalf("CreateInterviewStage: %v", err)
				}
			},
			count: func(t *testing.T, s *Store, applicationID int64) int {
				t.Helper()
				stages, err := s.ListInterviewStages(context.Background(), applicationID)
				if err != nil {
					t.Fatalf("ListInterviewStages: %v", err)
				}
				return len(stages)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			application := mustCreateApplication(t, s, posting.ID)
			tt.seed(t, s, application.ID)
			if tt.count(t, s, application.ID) == 0 {
				t.Fatalf("seeded nothing to delete")
			}

			if err := s.DeleteApplication(context.Background(), application.ID); err != nil {
				t.Fatalf("DeleteApplication: %v", err)
			}

			if got := tt.count(t, s, application.ID); got != 0 {
				t.Errorf("%d rows left for the deleted application, want 0", got)
			}
		})
	}
}

func TestDeleteApplication_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	if err := s.DeleteApplication(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteApplication error = %v, want ErrNotFound", err)
	}
}

// Deleting an application leaves its posting exactly as it was, and the
// posting can be applied to again from scratch (#232).
func TestDeleteApplication_LeavesThePostingAndAllowsStartingAgain(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := s.SetPostingInterested(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	application := mustCreateApplication(t, s, posting.ID)
	postingBefore, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	markupBefore, err := s.GetPostingMarkup(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
	}

	if err := s.DeleteApplication(ctx, application.ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}

	postingAfter, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting after delete: %v", err)
	}
	if diff := cmp.Diff(postingBefore, postingAfter); diff != "" {
		t.Errorf("posting changed (-before +after):\n%s", diff)
	}
	markupAfter, err := s.GetPostingMarkup(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPostingMarkup after delete: %v", err)
	}
	if diff := cmp.Diff(markupBefore, markupAfter); diff != "" {
		t.Errorf("posting markup changed (-before +after):\n%s", diff)
	}

	again, err := s.CreateApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication after delete: %v", err)
	}
	history, err := s.ListApplicationStatusHistory(ctx, again.ID)
	if err != nil {
		t.Fatalf("ListApplicationStatusHistory: %v", err)
	}
	if len(history) != 1 || history[0].Status != ApplicationStatusStarted {
		t.Errorf("history of the new application = %+v, want just its own started row", history)
	}
}
