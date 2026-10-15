package application_test

import (
	"context"
	"errors"
	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/docrecord"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/postingmarkup"
	"github.com/dklassen/swamp/store"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/applicationstatus"
)

func TestCreateApplication_ThenGet_ReturnsSameApplication(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	created, err := createApplication(ctx, s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	if created.Status != applicationstatus.Started {
		t.Fatalf("Status = %s, want %s", created.Status, applicationstatus.Started)
	}

	got, err := application.Get(ctx, s.DB(), posting.ID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("application.Get mismatch (-created +got):\n%s", diff)
	}
}

func TestGetApplication_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := application.Get(ctx, s.DB(), 999)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("application.Get error = %v, want store.ErrNotFound", err)
	}
}

func TestUpdateApplicationStatus_UpdatesStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)

	updated, err := updateApplicationStatus(ctx, s, posting.ID, applicationstatus.Interviewing)
	if err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}
	if updated.Status != applicationstatus.Interviewing {
		t.Fatalf("Status = %s, want %s", updated.Status, applicationstatus.Interviewing)
	}

	got, err := application.Get(ctx, s.DB(), posting.ID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if diff := cmp.Diff(updated, got); diff != "" {
		t.Fatalf("application.Get mismatch (-updated +got):\n%s", diff)
	}
}

func TestUpdateApplicationStatus_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := updateApplicationStatus(ctx, s, 999, applicationstatus.Interviewing)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("application.UpdateStatus error = %v, want store.ErrNotFound", err)
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

	if _, err := application.Get(ctx, s.DB(), posting.ID); err == nil {
		t.Fatal("application.Get with NULL status in DB = nil error, want an error")
	}
}

func TestUpdateApplicationNotes_UpdatesNotes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)

	updated, err := application.UpdateNotes(ctx, s.DB(), posting.ID, "Follow up next week")
	if err != nil {
		t.Fatalf("application.UpdateNotes: %v", err)
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

	got, err := application.GetByID(ctx, s.DB(), created.ID)
	if err != nil {
		t.Fatalf("application.GetByID: %v", err)
	}
	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("application.GetByID mismatch (-created +got):\n%s", diff)
	}
}

// TestGetApplicationByID_NonexistentID_ReturnsErrNotFound is the whole
// point of this lookup existing: callers holding only an application ID
// (`swamp export <application-id>`) need to tell an ID that names no row
// from one whose application simply has nothing drafted yet.
func TestGetApplicationByID_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := application.GetByID(ctx, s.DB(), 999)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("application.GetByID error = %v, want store.ErrNotFound", err)
	}
}

// TestGetApplicationByIDIncludingDeleted lets a caller holding an old ID
// tell an application the user deleted from one that never existed.
func TestGetApplicationByIDIncludingDeleted(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	live := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer").ID)
	deleted := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-2", "Designer").ID)
	if err := application.Delete(ctx, s.DB(), deleted.ID); err != nil {
		t.Fatalf("application.Delete: %v", err)
	}

	t.Run("live", func(t *testing.T) {
		t.Parallel()
		got, err := application.GetByIDIncludingDeleted(ctx, s.DB(), live.ID)
		if err != nil {
			t.Fatalf("application.GetByIDIncludingDeleted: %v", err)
		}
		if diff := cmp.Diff(live, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("deleted", func(t *testing.T) {
		t.Parallel()
		got, err := application.GetByIDIncludingDeleted(ctx, s.DB(), deleted.ID)
		if err != nil {
			t.Fatalf("application.GetByIDIncludingDeleted: %v", err)
		}
		if got.DeletedAt.IsZero() {
			t.Error("DeletedAt is zero, want the time it was deleted")
		}
		got.DeletedAt = time.Time{}
		if diff := cmp.Diff(deleted, got); diff != "" {
			t.Errorf("mismatch apart from DeletedAt (-want +got):\n%s", diff)
		}
	})
	t.Run("never existed", func(t *testing.T) {
		t.Parallel()
		if _, err := application.GetByIDIncludingDeleted(ctx, s.DB(), 999); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("err = %v, want store.ErrNotFound", err)
		}
	})
}

// TestCreateApplication_ClosedPosting_Refused: an application started on
// a closed posting can't be submitted, and sync never ends it, because it
// only ends applications when their posting changes to closed. So
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

	if _, err := createApplication(ctx, s, posting.ID); !errors.Is(err, application.ErrPostingClosed) {
		t.Fatalf("application.Create on a closed posting: err = %v, want application.ErrPostingClosed", err)
	}
	if _, err := application.Get(ctx, s.DB(), posting.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("application.Get after the refusal: err = %v, want store.ErrNotFound", err)
	}
}

// TestCreateApplication_NonexistentPosting_ReturnsErrNotFound: foreign
// keys aren't enforced, so without the posting check Create
// would start an application for a posting that doesn't exist.
func TestCreateApplication_NonexistentPosting_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	if _, err := createApplication(context.Background(), s, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("application.Create(9999): err = %v, want store.ErrNotFound", err)
	}
}

func TestDeleteApplication_ThenGet_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)

	if err := application.Delete(ctx, s.DB(), app.ID); err != nil {
		t.Fatalf("application.Delete: %v", err)
	}

	if _, err := application.Get(ctx, s.DB(), posting.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("application.Get error = %v, want store.ErrNotFound", err)
	}
}

// Document folders and agents' conversations refer to an application by
// ID, so a deleted ID handed to a new application would give it the old
// one's drafts.
func TestDeleteApplication_IDNotReusedByNextApplication(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	first := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	second := mustUpsertPosting(t, s, acme.ID, "job-2", "Staff Engineer")
	deleted := mustCreateApplication(t, s, first.ID)

	if err := application.Delete(ctx, s.DB(), deleted.ID); err != nil {
		t.Fatalf("application.Delete: %v", err)
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
		update func(s testStore, postingID int64) (application.Application, error)
	}{
		{
			name: "status",
			update: func(s testStore, postingID int64) (application.Application, error) {
				return updateApplicationStatus(context.Background(), s, postingID, applicationstatus.Submitted)
			},
		},
		{
			name: "notes",
			update: func(s testStore, postingID int64) (application.Application, error) {
				return application.UpdateNotes(context.Background(), s.DB(), postingID, "second try")
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
			if err := application.Delete(context.Background(), s.DB(), deleted.ID); err != nil {
				t.Fatalf("application.Delete: %v", err)
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
	search := func(order store.PostingOrder) func(s testStore) ([]int64, error) {
		return func(s testStore) ([]int64, error) {
			page, err := s.SearchPostings(context.Background(), store.PostingSearch{Order: order, Limit: 10})
			var ids []int64
			for _, l := range page.Listings {
				ids = append(ids, l.ApplicationID)
			}
			return ids, err
		}
	}
	tests := []struct {
		name           string
		applicationIDs func(s testStore) ([]int64, error)
	}{
		{
			name: "interested postings",
			applicationIDs: func(s testStore) ([]int64, error) {
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
		{name: "search by id", applicationIDs: search(store.PostingOrderIDAsc)},
		{name: "search by id, descending", applicationIDs: search(store.PostingOrderIDDesc)},
		{name: "search by published date", applicationIDs: search(store.PostingOrderPublishedDesc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			if _, err := postingmarkup.SetInterested(ctx, s.DB(), posting.ID); err != nil {
				t.Fatalf("SetPostingInterested: %v", err)
			}
			deleted := mustCreateApplication(t, s, posting.ID)
			if err := application.Delete(ctx, s.DB(), deleted.ID); err != nil {
				t.Fatalf("application.Delete: %v", err)
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

// Deleting an application is a soft delete: what it owns stays,
// for a later restore, and is no longer reachable from the posting.
func TestDeleteApplication_KeepsWhatTheApplicationOwns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		seed  func(t *testing.T, s testStore, applicationID int64)
		count func(t *testing.T, s testStore, applicationID int64) int
	}{
		{
			name: "status history",
			seed: func(*testing.T, testStore, int64) {}, // CreateApplication records one
			count: func(t *testing.T, s testStore, applicationID int64) int {
				t.Helper()
				history, err := application.ListStatusHistory(context.Background(), s.DB(), applicationID)
				if err != nil {
					t.Fatalf("application.ListStatusHistory: %v", err)
				}
				return len(history)
			},
		},
		{
			name: "document reviews",
			seed: func(t *testing.T, s testStore, applicationID int64) {
				t.Helper()
				ctx := context.Background()
				if err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
					_, err := docrecord.CreateReview(ctx, tx, applicationID, documents.Resume, "# Resume\n", docrecord.Passed, "")
					return err
				}); err != nil {
					t.Fatalf("docrecord.CreateReview: %v", err)
				}
			},
			count: func(t *testing.T, s testStore, applicationID int64) int {
				t.Helper()
				return s.countRows(t, "document_reviews", applicationID)
			},
		},
		{
			name: "document exports",
			seed: func(t *testing.T, s testStore, applicationID int64) {
				t.Helper()
				if err := docrecord.RecordExport(context.Background(), s.DB(), applicationID, documents.Resume, "# Resume\n", "/exports/resume.pdf"); err != nil {
					t.Fatalf("docrecord.RecordExport: %v", err)
				}
			},
			count: func(t *testing.T, s testStore, applicationID int64) int {
				t.Helper()
				return s.countRows(t, "document_exports", applicationID)
			},
		},
		{
			name: "interview stages",
			// No API writes interview_stages any more, but rows from before
			// it was removed must still survive a delete.
			seed: func(t *testing.T, s testStore, applicationID int64) {
				t.Helper()
				if _, err := s.sqlDB.Exec(`INSERT INTO interview_stages (application_id, sequence, name) VALUES (?, 1, 'Phone screen')`, applicationID); err != nil {
					t.Fatalf("insert interview stage: %v", err)
				}
			},
			count: func(t *testing.T, s testStore, applicationID int64) int {
				t.Helper()
				var n int
				if err := s.sqlDB.QueryRow(`SELECT count(*) FROM interview_stages WHERE application_id = ?`, applicationID).Scan(&n); err != nil {
					t.Fatalf("count interview stages: %v", err)
				}
				return n
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			app := mustCreateApplication(t, s, posting.ID)
			tt.seed(t, s, app.ID)
			before := tt.count(t, s, app.ID)
			if before == 0 {
				t.Fatalf("seeded nothing to keep")
			}

			if err := application.Delete(context.Background(), s.DB(), app.ID); err != nil {
				t.Fatalf("application.Delete: %v", err)
			}

			if got := tt.count(t, s, app.ID); got != before {
				t.Errorf("%d rows left for the deleted application, want %d", got, before)
			}
		})
	}
}

func TestDeleteApplication_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	if err := application.Delete(context.Background(), s.DB(), 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("application.Delete error = %v, want store.ErrNotFound", err)
	}
}

// Deleting an application leaves its posting exactly as it was, and the
// posting can be applied to again from scratch.
func TestDeleteApplication_LeavesThePostingAndAllowsStartingAgain(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := postingmarkup.SetInterested(ctx, s.DB(), posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	app := mustCreateApplication(t, s, posting.ID)
	postingBefore, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	markupBefore, err := postingmarkup.Get(ctx, s.DB(), posting.ID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
	}

	if err := application.Delete(ctx, s.DB(), app.ID); err != nil {
		t.Fatalf("application.Delete: %v", err)
	}

	postingAfter, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting after delete: %v", err)
	}
	if diff := cmp.Diff(postingBefore, postingAfter); diff != "" {
		t.Errorf("posting changed (-before +after):\n%s", diff)
	}
	markupAfter, err := postingmarkup.Get(ctx, s.DB(), posting.ID)
	if err != nil {
		t.Fatalf("GetPostingMarkup after delete: %v", err)
	}
	if diff := cmp.Diff(markupBefore, markupAfter); diff != "" {
		t.Errorf("posting markup changed (-before +after):\n%s", diff)
	}

	again, err := createApplication(ctx, s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create after delete: %v", err)
	}
	history, err := application.ListStatusHistory(ctx, s.DB(), again.ID)
	if err != nil {
		t.Fatalf("application.ListStatusHistory: %v", err)
	}
	if len(history) != 1 || history[0].Status != applicationstatus.Started {
		t.Errorf("history of the new app = %+v, want just its own started row", history)
	}
}
