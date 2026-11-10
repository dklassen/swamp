package stage

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

func newTestStage(t *testing.T) (*Stage, testutil.TestApp) {
	t.Helper()
	s := testutil.NewTestApp(t)
	return New(s.Companies, s.Postings, s.Documents, s.Scores, s.Drafting), s
}

func mustCreateCompany(t *testing.T, s testutil.TestApp, name string) company.Company {
	t.Helper()
	c, err := s.Companies.Add(context.Background(), name, jobboard.Ashby, name)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return *c
}

func mustUpsertPosting(t *testing.T, s testutil.TestApp, companyID int64, sourceID, title string) posting.Posting {
	t.Helper()
	ingested, err := s.Postings.Ingest(context.Background(), companyID, jobboard.Ashby, sourceID, posting.IngestedFields{
		Title:      title,
		RawPayload: `{"id":"` + sourceID + `"}`,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return ingested.Posting
}

func mustMarkInterested(t *testing.T, s testutil.TestApp, postingID int64) {
	t.Helper()
	if _, err := s.Postings.SetInterested(context.Background(), postingID); err != nil {
		t.Fatalf("postingmarkup.SetInterested: %v", err)
	}
}

// jsonKeys marshals v and returns its top-level JSON object's keys,
// sorted -- used to pin the field-name set stage.Candidate/Prepared
// serialize to for the external agent hand-off
// without being fragile to dynamic values like timestamps, which
// a literal golden-string comparison would be.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestCandidate_JSONShape_MatchesDocumentedAgentContract pins the exact
// field-name set stage.Candidate serializes to -- the JSON shape
// .agents/skills/apply-to-posting/SKILL.md documents for `stage list`.
// A Go-side field rename with no matching json tag update would fail
// this test instead of silently breaking the agent hand-off (see
// #59).
func TestCandidate_JSONShape_MatchesDocumentedAgentContract(t *testing.T) {
	t.Parallel()

	st, s := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)

	got, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}

	wantTop := []string{"Posting", "CompanyName", "ApplicationID", "ApplicationStatus", "ApplicationNotes", "LatestReviews"}
	sort.Strings(wantTop)
	if diff := cmp.Diff(wantTop, jsonKeys(t, got[0])); diff != "" {
		t.Fatalf("Candidate top-level JSON keys mismatch (-want +got):\n%s", diff)
	}

	// A summary, enough to pick a posting: no description or raw payload,
	// which made the list too large for agent clients.
	wantPosting := []string{"ID", "Title", "Department", "Location", "WorkplaceType", "ApplicationURL", "PublishedAt", "FirstSeenAt"}
	sort.Strings(wantPosting)
	if diff := cmp.Diff(wantPosting, jsonKeys(t, got[0].Posting)); diff != "" {
		t.Fatalf("Candidate.Posting JSON keys mismatch (-want +got):\n%s", diff)
	}
}

// TestPrepared_JSONShape_MatchesDocumentedAgentContract is
// TestCandidate_JSONShape_MatchesDocumentedAgentContract for `stage
// prepare`'s output shape.
func TestPrepared_JSONShape_MatchesDocumentedAgentContract(t *testing.T) {
	t.Parallel()

	st, s := newTestStage(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Engineer")
	mustMarkInterested(t, s, posting.ID)

	got, err := st.Prepare(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	// ApplicationFormError is omitted when empty.
	wantTop := []string{"Posting", "CompanyName", "ApplicationID", "Documents", "ApplicationNotes", "LatestReviews", "ApplicationForm"}
	sort.Strings(wantTop)
	if diff := cmp.Diff(wantTop, jsonKeys(t, got)); diff != "" {
		t.Fatalf("Prepared top-level JSON keys mismatch (-want +got):\n%s", diff)
	}

	wantDocument := []string{"Path", "Exists", "Version"}
	sort.Strings(wantDocument)
	// One entry per document type, keyed by its name (RFC 0004).
	var wantTypes []string
	for _, documentType := range document.Types() {
		wantTypes = append(wantTypes, documentType.String())
	}
	sort.Strings(wantTypes)
	if diff := cmp.Diff(wantTypes, jsonKeys(t, got.Documents)); diff != "" {
		t.Fatalf("Prepared.Documents JSON keys mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantDocument, jsonKeys(t, got.Documents[document.CoverLetter])); diff != "" {
		t.Fatalf("Prepared.Documents[cover_letter] JSON keys mismatch (-want +got):\n%s", diff)
	}

	// Everything needed to draft, but not RawPayload or DescriptionHTML:
	// drafting reads DescriptionText, and the other two were most of the
	// response.
	wantPosting := []string{
		"ID", "CompanyID", "Source", "SourceID", "Title", "Department", "Team",
		"Location", "EmploymentType", "WorkplaceType", "DescriptionText",
		"JobURL", "ApplicationURL", "PublishedAt", "ListingStatus",
		"FirstSeenAt", "LastSeenAt", "CreatedAt", "UpdatedAt",
	}
	sort.Strings(wantPosting)
	if diff := cmp.Diff(wantPosting, jsonKeys(t, got.Posting)); diff != "" {
		t.Fatalf("Prepared.Posting JSON keys mismatch (-want +got):\n%s", diff)
	}
}

// fakeDrafting queues queue and prepares every posting as prepared.
type fakeDrafting struct {
	queue    []service.QueuedPosting
	prepared *service.Prepared
}

func (f fakeDrafting) Queue(context.Context) ([]service.QueuedPosting, error) {
	return f.queue, nil
}

func (f fakeDrafting) Prepare(context.Context, int64) (*service.Prepared, error) {
	return f.prepared, nil
}

func TestPrepare_MapsWhatDraftingPrepared(t *testing.T) {
	t.Parallel()
	reviewedAt := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		form          service.FormRead
		wantForm      *jobboard.ApplicationForm
		wantFormError string
	}{
		{name: "form read", form: service.FormRead{Form: &testForm}, wantForm: &testForm},
		{name: "fetch failed", form: service.FormRead{FetchError: "board unavailable"}, wantFormError: "board unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prepared := &service.Prepared{
				Posting:     posting.Posting{ID: 7, CompanyID: 3, Source: jobboard.Greenhouse, SourceID: "job-1", IngestedFields: posting.IngestedFields{Title: "Engineer", DescriptionText: "Build things"}},
				Company:     company.Company{ID: 3, Name: "Acme"},
				Application: application.Application{ID: 11, PostingID: 7, Status: application.Started, Notes: "referred by a friend"},
				Documents: map[document.Type]service.DraftDocument{
					document.CoverLetter: {Path: "/docs/11/cover_letter.md", Exists: true, Version: "abc"},
					document.Resume:      {Path: "/docs/11/resume.md", Version: service.NoDocumentYet},
				},
				LatestReviews: map[document.Type]document.Review{
					document.CoverLetter: {ID: 5, ApplicationID: 11, DocumentType: document.CoverLetter, Cycle: 2, ContentSHA256: "abc", Outcome: document.Flagged, Notes: "too generic", CreatedAt: reviewedAt},
				},
				Form: tt.form,
			}
			st := New(nil, nil, nil, nil, fakeDrafting{prepared: prepared})

			got, err := st.Prepare(t.Context(), 7)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			want := &Prepared{
				Posting:       PreparedPosting{ID: 7, CompanyID: 3, Source: jobboard.Greenhouse, SourceID: "job-1", Title: "Engineer", DescriptionText: "Build things"},
				CompanyName:   "Acme",
				ApplicationID: 11,
				Documents: map[document.Type]Document{
					document.CoverLetter: {Path: "/docs/11/cover_letter.md", Exists: true, Version: "abc"},
					document.Resume:      {Path: "/docs/11/resume.md", Version: service.NoDocumentYet},
				},
				ApplicationNotes:     "referred by a friend",
				LatestReviews:        map[document.Type]LatestReview{document.CoverLetter: {Outcome: document.Flagged, Notes: "too generic", Cycle: 2, CreatedAt: reviewedAt}},
				ApplicationForm:      tt.wantForm,
				ApplicationFormError: tt.wantFormError,
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Prepare (-want +got):\n%s", diff)
			}
		})
	}
}

func TestList_MapsTheDraftingQueue(t *testing.T) {
	t.Parallel()
	reviewedAt := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	firstSeen := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	interested := posting.Posting{ID: 7, CompanyID: 3, IngestedFields: posting.IngestedFields{Title: "Engineer", Location: "Remote", DescriptionText: "Build things"}, FirstSeenAt: firstSeen}
	started := posting.Posting{ID: 8, CompanyID: 4, IngestedFields: posting.IngestedFields{Title: "Designer"}, FirstSeenAt: firstSeen}
	queue := []service.QueuedPosting{
		{Posting: interested, CompanyName: "Acme"},
		{
			Posting:     started,
			CompanyName: "Globex",
			Application: &application.Application{ID: 11, PostingID: 8, Status: application.Started, Notes: "referred by a friend"},
			LatestReviews: map[document.Type]document.Review{
				document.Resume: {ID: 5, ApplicationID: 11, DocumentType: document.Resume, Cycle: 2, Outcome: document.Flagged, Notes: "too long", CreatedAt: reviewedAt},
			},
		},
	}
	st := New(nil, nil, nil, nil, fakeDrafting{queue: queue})

	got, err := st.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	applicationID, status := int64(11), application.Started
	want := []Candidate{
		{
			Posting:       PostingSummary{ID: 7, Title: "Engineer", Location: "Remote", FirstSeenAt: firstSeen},
			CompanyName:   "Acme",
			LatestReviews: map[document.Type]LatestReview{},
		},
		{
			Posting:           PostingSummary{ID: 8, Title: "Designer", FirstSeenAt: firstSeen},
			CompanyName:       "Globex",
			ApplicationID:     &applicationID,
			ApplicationStatus: &status,
			ApplicationNotes:  "referred by a friend",
			LatestReviews: map[document.Type]LatestReview{
				document.Resume: {Outcome: document.Flagged, Notes: "too long", Cycle: 2, CreatedAt: reviewedAt},
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("List (-want +got):\n%s", diff)
	}
}
