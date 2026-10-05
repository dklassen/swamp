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

// Document folders and agents' conversations refer to an application by
// ID, so a deleted ID handed to a new application would give it the old
// one's drafts (#244).
func TestDeleteApplication_IDNotReusedByNextApplication(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	first := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	second := mustUpsertPosting(t, s, acme.ID, "job-2", "Staff Engineer")
	deleted := mustCreateApplication(t, s, first.ID)

	if err := s.DeleteApplication(ctx, deleted.ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}
	next := mustCreateApplication(t, s, second.ID)

	if next.ID == deleted.ID {
		t.Errorf("new application got deleted application's ID %d", deleted.ID)
	}
}

// A posting whose application was deleted can start a fresh one, and
// updates by posting go to that one alone, not the deleted one too.
func TestUpdateApplication_AfterDeleteAndRestart_UpdatesOnlyTheLiveOne(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		update func(s *Store, postingID int64) (Application, error)
	}{
		{
			name: "status",
			update: func(s *Store, postingID int64) (Application, error) {
				return s.UpdateApplicationStatus(context.Background(), postingID, ApplicationStatusSubmitted)
			},
		},
		{
			name: "notes",
			update: func(s *Store, postingID int64) (Application, error) {
				return s.UpdateApplicationNotes(context.Background(), postingID, "second try")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			deleted := mustCreateApplication(t, s, posting.ID)
			if err := s.DeleteApplication(context.Background(), deleted.ID); err != nil {
				t.Fatalf("DeleteApplication: %v", err)
			}
			live := mustCreateApplication(t, s, posting.ID)

			got, err := tt.update(s, posting.ID)
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			if got.ID != live.ID {
				t.Errorf("updated application %d, want the live one %d", got.ID, live.ID)
			}
		})
	}
}

// Every listing that joins a posting to its application shows the posting
// once, with the live application, never the deleted one.
func TestListings_AfterDeleteAndRestart_ShowThePostingOnceWithTheLiveApplication(t *testing.T) {
	t.Parallel()
	search := func(order PostingOrder) func(s *Store) ([]int64, error) {
		return func(s *Store) ([]int64, error) {
			page, err := s.SearchPostings(context.Background(), PostingSearch{Order: order, Limit: 10})
			var ids []int64
			for _, l := range page.Listings {
				ids = append(ids, l.ApplicationID)
			}
			return ids, err
		}
	}
	tests := []struct {
		name           string
		applicationIDs func(s *Store) ([]int64, error)
	}{
		{
			name: "interested postings",
			applicationIDs: func(s *Store) ([]int64, error) {
				postings, err := s.ListInterestedPostings(context.Background())
				var ids []int64
				for _, p := range postings {
					if p.ApplicationID != nil {
						ids = append(ids, *p.ApplicationID)
					}
				}
				return ids, err
			},
		},
		{name: "search by id", applicationIDs: search(PostingOrderIDAsc)},
		{name: "search by id, descending", applicationIDs: search(PostingOrderIDDesc)},
		{name: "search by published date", applicationIDs: search(PostingOrderPublishedDesc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			if _, err := s.SetPostingInterested(ctx, posting.ID); err != nil {
				t.Fatalf("SetPostingInterested: %v", err)
			}
			deleted := mustCreateApplication(t, s, posting.ID)
			if err := s.DeleteApplication(ctx, deleted.ID); err != nil {
				t.Fatalf("DeleteApplication: %v", err)
			}
			live := mustCreateApplication(t, s, posting.ID)

			got, err := tt.applicationIDs(s)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if diff := cmp.Diff([]int64{live.ID}, got); diff != "" {
				t.Errorf("application IDs listed (-want +got):\n%s", diff)
			}
		})
	}
}

// Deleting an application is a soft delete (#244): what it owns stays,
// for a later restore, and is no longer reachable from the posting.
func TestDeleteApplication_KeepsWhatTheApplicationOwns(t *testing.T) {
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
			before := tt.count(t, s, application.ID)
			if before == 0 {
				t.Fatalf("seeded nothing to keep")
			}

			if err := s.DeleteApplication(context.Background(), application.ID); err != nil {
				t.Fatalf("DeleteApplication: %v", err)
			}

			if got := tt.count(t, s, application.ID); got != before {
				t.Errorf("%d rows left for the deleted application, want %d", got, before)
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
