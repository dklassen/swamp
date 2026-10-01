package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestUpsertPosting_NewPosting_ThenGet_ReturnsSamePosting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	created, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Software Engineer",
			RawPayload: `{"id":"job-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	got, err := s.GetPosting(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetPosting mismatch (-created +got):\n%s", diff)
	}
}

// TestUpsertPosting_PublishedAt_RoundTripsInAnyZone checks a posting saves
// and reads back the same PublishedAt instant whatever zone it was parsed
// into. Go only names a parsed offset's zone (EDT) when it matches the
// machine's local timezone; otherwise the zone is unnamed, as it is for
// Greenhouse's "-04:00" on a UTC machine. Stored as time.Time.String(),
// that became "-0400 -0400", which the driver can't read back -- see
// issue #140.
func TestUpsertPosting_PublishedAt_RoundTripsInAnyZone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		loc  *time.Location
	}{
		{name: "UTC", loc: time.UTC},
		{name: "named zone", loc: time.FixedZone("EDT", -4*60*60)},
		{name: "unnamed negative offset", loc: time.FixedZone("", -4*60*60)},
		{name: "unnamed positive offset", loc: time.FixedZone("", 5*60*60+30*60)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestStore(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "greenhouse", "acme")
			want := time.Date(2026, 8, 24, 13, 15, 0, 0, tt.loc)

			created, err := s.UpsertPosting(ctx, CreatePostingParams{
				CompanyID:      acme.ID,
				Source:         "greenhouse",
				SourceID:       "job-1",
				IngestedFields: IngestedFields{Title: "Engineer", PublishedAt: OptionalTime{Time: want}},
			})
			if err != nil {
				t.Fatalf("UpsertPosting: %v", err)
			}
			got, err := s.GetPosting(ctx, created.ID)
			if err != nil {
				t.Fatalf("GetPosting: %v", err)
			}
			if !got.PublishedAt.Equal(want) {
				t.Errorf("PublishedAt = %v, want the same instant as %v", got.PublishedAt, want)
			}
		})
	}
}

func TestUpsertPosting_NewPosting_AutoCreatesMarkupRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	created, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Software Engineer",
			RawPayload: `{"id":"job-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	markup, err := s.GetPostingMarkup(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
	}

	want := PostingMarkup{
		PostingID: created.ID,
		Notes:     "",
		CreatedAt: markup.CreatedAt,
		UpdatedAt: markup.UpdatedAt,
	}
	if diff := cmp.Diff(want, markup); diff != "" {
		t.Fatalf("GetPostingMarkup mismatch (-want +got):\n%s", diff)
	}
}

func TestUpsertPosting_SameSourceAndSourceID_UpdatesInPlace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	first, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Software Engineer",
			RawPayload: `{"id":"job-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting (first): %v", err)
	}

	second, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Senior Software Engineer",
			RawPayload: `{"id":"job-1","title":"Senior Software Engineer"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting (second): %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("UpsertPosting (second).ID = %d, want %d (same posting)", second.ID, first.ID)
	}
	if second.Title != "Senior Software Engineer" {
		t.Fatalf("UpsertPosting (second).Title = %q, want %q", second.Title, "Senior Software Engineer")
	}

	all, err := s.ListPostingsByCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListPostingsByCompany = %d postings, want 1 (upsert should not duplicate)", len(all))
	}
}

func TestUpsertPosting_OnUpdate_DoesNotChangeListingStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}

	updated, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Software Engineer II",
			RawPayload: `{"id":"job-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	if updated.ListingStatus != "closed" {
		t.Fatalf("UpsertPosting.ListingStatus = %q, want %q (upsert must not reopen)", updated.ListingStatus, "closed")
	}
}

func TestGetPosting_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPosting(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPosting error = %v, want ErrNotFound", err)
	}
}

func TestGetPostingBySourceAndSourceID_ReturnsMatchingPosting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	created := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	got, err := s.GetPostingBySourceAndSourceID(ctx, "ashby", "job-1")
	if err != nil {
		t.Fatalf("GetPostingBySourceAndSourceID: %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetPostingBySourceAndSourceID mismatch (-created +got):\n%s", diff)
	}
}

func TestGetPostingBySourceAndSourceID_NoMatch_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPostingBySourceAndSourceID(ctx, "ashby", "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPostingBySourceAndSourceID error = %v, want ErrNotFound", err)
	}
}

func TestListPostingsByCompany_OnlyReturnsPostingsForThatCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")

	acmePosting := mustUpsertPosting(t, s, acme.ID, "job-1", "Acme Engineer")
	mustUpsertPosting(t, s, globex.ID, "job-2", "Globex Engineer")

	got, err := s.ListPostingsByCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}

	want := []Posting{acmePosting}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListPostingsByCompany mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkPostingClosed_SetsListingStatusClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}

	got, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if got.ListingStatus != "closed" {
		t.Fatalf("ListingStatus = %q, want %q", got.ListingStatus, "closed")
	}
}

func TestClosePosting(t *testing.T) {
	t.Parallel()
	closeFrom := []ApplicationStatus{ApplicationStatusStarted, ApplicationStatusSubmitted}

	tests := []struct {
		name          string
		alreadyClosed bool
		application   *ApplicationStatus // nil: the posting has no application
		want          ClosePostingResult
		wantApp       ApplicationStatus
		wantHistory   int
	}{
		{name: "open, no application", want: ClosePostingResult{Closed: true}, wantHistory: 1},
		{name: "open, application at a status it ends", application: ptr(ApplicationStatusStarted),
			want: ClosePostingResult{Closed: true, ApplicationClosed: true}, wantApp: ApplicationStatusPostingClosed, wantHistory: 1},
		{name: "open, application at a status it leaves alone", application: ptr(ApplicationStatusInterviewing),
			want: ClosePostingResult{Closed: true}, wantApp: ApplicationStatusInterviewing, wantHistory: 1},
		{name: "already closed: changes nothing", alreadyClosed: true, application: ptr(ApplicationStatusStarted),
			want: ClosePostingResult{}, wantApp: ApplicationStatusStarted, wantHistory: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			if tt.application != nil {
				if _, err := s.CreateApplication(ctx, posting.ID); err != nil {
					t.Fatalf("CreateApplication: %v", err)
				}
				if _, err := s.UpdateApplicationStatus(ctx, posting.ID, *tt.application); err != nil {
					t.Fatalf("UpdateApplicationStatus: %v", err)
				}
			}
			if tt.alreadyClosed {
				if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
					t.Fatalf("MarkPostingClosed: %v", err)
				}
			}

			got, err := s.ClosePosting(ctx, posting.ID, closeFrom)
			if err != nil {
				t.Fatalf("ClosePosting: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ClosePosting result mismatch (-want +got):\n%s", diff)
			}

			after, err := s.GetPosting(ctx, posting.ID)
			if err != nil {
				t.Fatalf("GetPosting: %v", err)
			}
			if after.ListingStatus != "closed" {
				t.Errorf("ListingStatus = %q, want closed", after.ListingStatus)
			}
			history, err := s.ListPostingHistory(ctx, posting.ID)
			if err != nil {
				t.Fatalf("ListPostingHistory: %v", err)
			}
			if len(history) != tt.wantHistory {
				t.Fatalf("history rows = %d, want %d", len(history), tt.wantHistory)
			}
			if tt.wantHistory == 1 {
				if history[0].ChangeType != "closed" || !strings.Contains(history[0].Snapshot, `"ListingStatus":"open"`) {
					t.Errorf("history = %q with snapshot %s, want a \"closed\" row snapshotting the posting as it was, still open", history[0].ChangeType, history[0].Snapshot)
				}
			}
			if tt.application != nil {
				application, err := s.GetApplication(ctx, posting.ID)
				if err != nil {
					t.Fatalf("GetApplication: %v", err)
				}
				if application.Status != tt.wantApp {
					t.Errorf("application status = %s, want %s", application.Status, tt.wantApp)
				}
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestIngestPosting(t *testing.T) {
	t.Parallel()
	params := func(title string) CreatePostingParams {
		return CreatePostingParams{
			Source:         "ashby",
			SourceID:       "job-1",
			IngestedFields: IngestedFields{Title: title, RawPayload: `{"id":"job-1"}`},
		}
	}

	tests := []struct {
		name        string
		stored      string // title already stored; "" means not stored yet
		ingest      string
		wantCreated bool
		wantUpdated bool
		wantHistory []string
	}{
		{name: "not stored: created", ingest: "Engineer", wantCreated: true},
		{name: "stored, same content: nothing changes", stored: "Engineer", ingest: "Engineer"},
		{name: "stored, new content: updated with history", stored: "Engineer", ingest: "Senior Engineer",
			wantUpdated: true, wantHistory: []string{"content_updated"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			var before Posting
			if tt.stored != "" {
				before = mustUpsertPosting(t, s, acme.ID, "job-1", tt.stored)
			}

			p := params(tt.ingest)
			p.CompanyID = acme.ID
			got, err := s.IngestPosting(ctx, p)
			if err != nil {
				t.Fatalf("IngestPosting: %v", err)
			}
			if got.Created != tt.wantCreated || got.Updated != tt.wantUpdated {
				t.Errorf("Created, Updated = %v, %v, want %v, %v", got.Created, got.Updated, tt.wantCreated, tt.wantUpdated)
			}
			if got.Posting.Title != tt.ingest {
				t.Errorf("Posting.Title = %q, want %q", got.Posting.Title, tt.ingest)
			}
			if tt.stored == tt.ingest && !got.Posting.UpdatedAt.Equal(before.UpdatedAt) {
				t.Errorf("UpdatedAt changed from %v to %v, want the row untouched", before.UpdatedAt, got.Posting.UpdatedAt)
			}

			history, err := s.ListPostingHistory(ctx, got.Posting.ID)
			if err != nil {
				t.Fatalf("ListPostingHistory: %v", err)
			}
			var changeTypes []string
			for _, h := range history {
				changeTypes = append(changeTypes, h.ChangeType)
				if !strings.Contains(h.Snapshot, `"Title":"`+tt.stored+`"`) {
					t.Errorf("history snapshot %s, want the stored version (Title %q)", h.Snapshot, tt.stored)
				}
			}
			if diff := cmp.Diff(tt.wantHistory, changeTypes); diff != "" {
				t.Errorf("history mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReopenPosting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		closed       bool
		wantReopened bool
		wantHistory  int
	}{
		{name: "closed: reopened with history", closed: true, wantReopened: true, wantHistory: 1},
		{name: "already open: changes nothing", closed: false, wantReopened: false, wantHistory: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			if tt.closed {
				if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
					t.Fatalf("MarkPostingClosed: %v", err)
				}
			}

			result, err := s.ReopenPosting(ctx, posting.ID)
			if err != nil {
				t.Fatalf("ReopenPosting: %v", err)
			}
			if result.Reopened != tt.wantReopened {
				t.Errorf("ReopenPosting Reopened = %v, want %v", result.Reopened, tt.wantReopened)
			}
			after, err := s.GetPosting(ctx, posting.ID)
			if err != nil {
				t.Fatalf("GetPosting: %v", err)
			}
			if after.ListingStatus != "open" {
				t.Errorf("ListingStatus = %q, want open", after.ListingStatus)
			}
			history, err := s.ListPostingHistory(ctx, posting.ID)
			if err != nil {
				t.Fatalf("ListPostingHistory: %v", err)
			}
			if len(history) != tt.wantHistory {
				t.Fatalf("history rows = %d, want %d", len(history), tt.wantHistory)
			}
			if tt.wantHistory == 1 && (history[0].ChangeType != "reopened" || !strings.Contains(history[0].Snapshot, `"ListingStatus":"closed"`)) {
				t.Errorf("history = %q with snapshot %s, want a \"reopened\" row snapshotting the posting while closed", history[0].ChangeType, history[0].Snapshot)
			}
		})
	}
}

func TestMarkPostingReopened_SetsListingStatusOpen(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if err := s.MarkPostingReopened(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingReopened: %v", err)
	}

	got, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if got.ListingStatus != "open" {
		t.Fatalf("ListingStatus = %q, want %q", got.ListingStatus, "open")
	}
}

func TestGetPostingMarkup_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPostingMarkup(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPostingMarkup error = %v, want ErrNotFound", err)
	}
}

func TestUnmarkPostingInterested_ClearsInterestedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if _, err := s.SetPostingInterested(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}

	updated, err := s.UnmarkPostingInterested(ctx, posting.ID)
	if err != nil {
		t.Fatalf("UnmarkPostingInterested: %v", err)
	}
	if updated.InterestedAt != nil {
		t.Fatalf("InterestedAt = %v, want nil", updated.InterestedAt)
	}
}

func TestUnarchivePosting_ClearsArchivedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if _, err := s.SetPostingArchived(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}

	updated, err := s.UnarchivePosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("UnarchivePosting: %v", err)
	}
	if updated.ArchivedAt != nil {
		t.Fatalf("ArchivedAt = %v, want nil", updated.ArchivedAt)
	}
}

func TestUpdatePostingMarkupNotes_UpdatesNotes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	updated, err := s.UpdatePostingMarkupNotes(ctx, posting.ID, "Looks like a great fit")
	if err != nil {
		t.Fatalf("UpdatePostingMarkupNotes: %v", err)
	}
	if updated.Notes != "Looks like a great fit" {
		t.Fatalf("Notes = %q, want %q", updated.Notes, "Looks like a great fit")
	}
}

func TestListDistinctDepartmentsForCompany_ReturnsSortedUniqueValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	eng := "Engineering"
	sales := "Sales"
	for i, dept := range []string{eng, sales, eng} {
		_, err := s.UpsertPosting(ctx, CreatePostingParams{
			CompanyID: acme.ID,
			Source:    "ashby",
			SourceID:  fmt.Sprintf("job-%d", i),
			IngestedFields: IngestedFields{
				Title:      "Role",
				Department: dept,
				RawPayload: "{}",
			},
		})
		if err != nil {
			t.Fatalf("UpsertPosting: %v", err)
		}
	}

	got, err := s.ListDistinctDepartmentsForCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListDistinctDepartmentsForCompany: %v", err)
	}
	want := []string{"Engineering", "Sales"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListDistinctDepartmentsForCompany mismatch (-want +got):\n%s", diff)
	}
}

func TestListDistinctLocationsForCompany_ReturnsSortedUniqueValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	remote := "Remote"
	nyc := "New York"
	for i, loc := range []string{nyc, remote, remote} {
		_, err := s.UpsertPosting(ctx, CreatePostingParams{
			CompanyID: acme.ID,
			Source:    "ashby",
			SourceID:  fmt.Sprintf("job-%d", i),
			IngestedFields: IngestedFields{
				Title:      "Role",
				Location:   loc,
				RawPayload: "{}",
			},
		})
		if err != nil {
			t.Fatalf("UpsertPosting: %v", err)
		}
	}

	got, err := s.ListDistinctLocationsForCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListDistinctLocationsForCompany: %v", err)
	}
	want := []string{"New York", "Remote"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListDistinctLocationsForCompany mismatch (-want +got):\n%s", diff)
	}
}

// TestPosting_MarshalJSON_ZeroPublishedAtSerializesAsNull covers #80.
// PublishedAt is an OptionalTime, whose zero value means "not known"
// (it is not a pointer -- see #67, nothing in Go distinguishes never-set
// from the zero value). The consumer of this JSON is an external agent,
// and "0001-01-01T00:00:00Z" is noise in that contract where null is a
// clean absence. Also catches OptionalTime.MarshalJSON being removed,
// which would silently degrade to the embedded time.Time's own.
func TestPosting_MarshalJSON_ZeroPublishedAtSerializesAsNull(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(Posting{ID: 1})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"PublishedAt":null`)) {
		t.Errorf("Posting with no publish date encoded as %s, want \"PublishedAt\":null", encoded)
	}
	// Scoped to PublishedAt: FirstSeenAt/CreatedAt/UpdatedAt are NOT NULL
	// columns that are always set in practice, so their zero value in a
	// synthetic struct is not the contract problem this fixes.
	if bytes.Contains(encoded, []byte(`"PublishedAt":"0001-01-01`)) {
		t.Errorf("Posting encoded as %s, want no zero-time sentinel for PublishedAt in the agent contract", encoded)
	}
}

func TestPosting_MarshalJSON_RealPublishedAtIsPreserved(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 8, 24, 13, 15, 0, 0, time.UTC)
	encoded, err := json.Marshal(Posting{ID: 1, IngestedFields: IngestedFields{PublishedAt: OptionalTime{Time: published}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"PublishedAt":"2026-08-24T13:15:00Z"`)) {
		t.Errorf("Posting encoded as %s, want the real publish date preserved", encoded)
	}
}

// TestPosting_MarshalJSON_KeepsEveryField guards against a struct-level
// MarshalJSON reappearing on Posting or on the IngestedFields it embeds.
// IngestedFields is embedded untagged so its fields stay promoted into
// Posting's JSON object (#59); a MarshalJSON on it would be promoted too
// and hijack the whole object, dropping ID, ListingStatus and the
// timestamps from the agent contract with no error. Encoding the
// optionality on OptionalTime instead of on a struct is what avoids
// that, and this test fails if anyone reintroduces the struct-level form.
func TestPosting_MarshalJSON_KeepsEveryField(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(Posting{ID: 7, IngestedFields: IngestedFields{Title: "Engineer"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{
		"ID", "CompanyID", "Source", "SourceID", "Title", "Department", "Team",
		"Location", "EmploymentType", "WorkplaceType", "DescriptionHTML",
		"DescriptionText", "JobURL", "ApplicationURL", "PublishedAt",
		"RawPayload", "ListingStatus", "FirstSeenAt", "LastSeenAt",
		"CreatedAt", "UpdatedAt",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("encoded Posting is missing key %q; got %s", key, encoded)
		}
	}
}

// Open means what a company's posting list shows by default: listings
// still open on the job board that the user hasn't archived.
func TestCountOpenPostingsByCompany_CountsOpenUnarchivedPerCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	closed := mustUpsertPosting(t, s, acme.ID, "job-3", "Closed role")
	if err := s.MarkPostingClosed(ctx, closed.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	archived := mustUpsertPosting(t, s, acme.ID, "job-4", "Archived role")
	if _, err := s.SetPostingArchived(ctx, archived.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}

	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")
	mustUpsertPosting(t, s, globex.ID, "globex-job-1", "Engineer")

	initech := mustCreateCompany(t, s, "Initech", "ashby", "initech")

	got, err := s.CountOpenPostingsByCompany(ctx)
	if err != nil {
		t.Fatalf("CountOpenPostingsByCompany: %v", err)
	}
	want := map[int64]int{acme.ID: 2, globex.ID: 1}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("counts mismatch (-want +got):\n%s", diff)
	}
	if n := got[initech.ID]; n != 0 {
		t.Errorf("Initech (no postings) count = %d, want 0", n)
	}
}

// TestReopenPosting_RestoresApplicationSyncClosed: a reopened posting
// undoes what its closing did to the application, and nothing else
// (#174). The application goes back to the status it had before sync
// moved it to posting_closed, recorded as sync's change, in the same
// transaction as the reopen. A posting_closed the user set, a status the
// user changed since, or a close recorded before changed_by existed is
// left alone.
func TestReopenPosting_RestoresApplicationSyncClosed(t *testing.T) {
	t.Parallel()
	closeFrom := []ApplicationStatus{ApplicationStatusStarted, ApplicationStatusSubmitted}

	tests := []struct {
		name string
		// setup leaves the posting closed.
		setup         func(t *testing.T, s *Store, postingID int64)
		want          ReopenPostingResult
		wantApp       ApplicationStatus
		wantLastBy    StatusChangedBy
		noApplication bool
	}{
		{
			name: "sync closed it: back to started",
			setup: func(t *testing.T, s *Store, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, closeFrom)
			},
			want:    ReopenPostingResult{Reopened: true, ApplicationRestored: true},
			wantApp: ApplicationStatusStarted, wantLastBy: StatusChangedBySync,
		},
		{
			name: "sync closed it from submitted: back to submitted",
			setup: func(t *testing.T, s *Store, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustUpdateApplicationStatus(t, s, postingID, ApplicationStatusSubmitted)
				mustClosePosting(t, s, postingID, closeFrom)
			},
			want:    ReopenPostingResult{Reopened: true, ApplicationRestored: true},
			wantApp: ApplicationStatusSubmitted, wantLastBy: StatusChangedBySync,
		},
		{
			name: "user set posting_closed: left alone",
			setup: func(t *testing.T, s *Store, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, nil)
				mustUpdateApplicationStatus(t, s, postingID, ApplicationStatusPostingClosed)
			},
			want:    ReopenPostingResult{Reopened: true},
			wantApp: ApplicationStatusPostingClosed, wantLastBy: StatusChangedByUser,
		},
		{
			name: "user changed it after sync closed it: left alone",
			setup: func(t *testing.T, s *Store, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, closeFrom)
				mustUpdateApplicationStatus(t, s, postingID, ApplicationStatusWithdrawn)
			},
			want:    ReopenPostingResult{Reopened: true},
			wantApp: ApplicationStatusWithdrawn, wantLastBy: StatusChangedByUser,
		},
		{
			name: "closed before changed_by existed: left alone",
			setup: func(t *testing.T, s *Store, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, closeFrom)
				if _, err := s.sqlDB.ExecContext(context.Background(), `UPDATE application_status_history SET changed_by = 'unknown'`); err != nil {
					t.Fatalf("mark history unknown: %v", err)
				}
			},
			want:    ReopenPostingResult{Reopened: true},
			wantApp: ApplicationStatusPostingClosed, wantLastBy: StatusChangedByUnknown,
		},
		{
			name: "no application",
			setup: func(t *testing.T, s *Store, postingID int64) {
				mustClosePosting(t, s, postingID, closeFrom)
			},
			want:          ReopenPostingResult{Reopened: true},
			noApplication: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			tt.setup(t, s, posting.ID)

			got, err := s.ReopenPosting(ctx, posting.ID)
			if err != nil {
				t.Fatalf("ReopenPosting: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ReopenPosting result mismatch (-want +got):\n%s", diff)
			}
			if tt.noApplication {
				return
			}
			application, err := s.GetApplication(ctx, posting.ID)
			if err != nil {
				t.Fatalf("GetApplication: %v", err)
			}
			if application.Status != tt.wantApp {
				t.Errorf("application status = %s, want %s", application.Status, tt.wantApp)
			}
			history, err := s.ListApplicationStatusHistory(ctx, application.ID)
			if err != nil {
				t.Fatalf("ListApplicationStatusHistory: %v", err)
			}
			last := history[len(history)-1]
			if last.Status != tt.wantApp || last.ChangedBy != tt.wantLastBy {
				t.Errorf("last history row = %s by %s, want %s by %s", last.Status, last.ChangedBy, tt.wantApp, tt.wantLastBy)
			}
		})
	}
}

func mustClosePosting(t *testing.T, s *Store, postingID int64, closeApplicationFrom []ApplicationStatus) {
	t.Helper()
	if _, err := s.ClosePosting(context.Background(), postingID, closeApplicationFrom); err != nil {
		t.Fatalf("ClosePosting: %v", err)
	}
}

func mustUpdateApplicationStatus(t *testing.T, s *Store, postingID int64, status ApplicationStatus) {
	t.Helper()
	if _, err := s.UpdateApplicationStatus(context.Background(), postingID, status); err != nil {
		t.Fatalf("UpdateApplicationStatus(%s): %v", status, err)
	}
}

// TestReopenPosting_RestoreFails_PostingStaysClosed: the reopen and the
// application restore are one change (#174), so a failed restore leaves
// the posting closed and unrecorded, and the next sync tries both again.
func TestReopenPosting_RestoreFails_PostingStaysClosed(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)
	mustClosePosting(t, s, posting.ID, []ApplicationStatus{ApplicationStatusStarted})
	if _, err := s.sqlDB.ExecContext(ctx, `CREATE TRIGGER fail_application_update BEFORE UPDATE ON applications
		BEGIN SELECT RAISE(ABORT, 'simulated failure'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := s.ReopenPosting(ctx, posting.ID); err == nil {
		t.Fatal("ReopenPosting with a failing application update: want an error, got nil")
	}

	after, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if after.ListingStatus != "closed" {
		t.Errorf("ListingStatus = %q, want still closed", after.ListingStatus)
	}
	history, err := s.ListPostingHistory(ctx, posting.ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if n := len(history); n != 1 || history[0].ChangeType != "closed" {
		t.Errorf("posting history = %d rows, want only the close", n)
	}
}
