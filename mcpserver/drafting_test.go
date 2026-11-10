package mcpserver

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

// keys returns obj's keys, sorted.
func keys[V any](obj map[string]V) []string {
	out := make([]string, 0, len(obj))
	for k := range obj {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// object unmarshals raw as a JSON object.
func object(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return obj
}

// The skills read these field names, so a Go-side rename has to fail a
// test rather than silently change them.
func TestListPostings_JSONShape(t *testing.T) {
	t.Parallel()
	srv, s, _ := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	mustMarkInterested(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	cs := connectClient(t, srv)

	got := callTool[struct{ Postings []json.RawMessage }](t, cs, "list_postings", map[string]any{}).Postings
	if len(got) != 1 {
		t.Fatalf("got %d postings, want 1", len(got))
	}
	candidate := object(t, got[0])
	wantTop := []string{"ApplicationID", "ApplicationNotes", "ApplicationStatus", "CompanyName", "LatestReviews", "Posting"}
	if diff := cmp.Diff(wantTop, keys(candidate)); diff != "" {
		t.Errorf("top-level keys (-want +got):\n%s", diff)
	}
	// A summary, enough to pick a posting: no description or raw payload,
	// which made the list too large for agent clients.
	wantPosting := []string{"ApplicationURL", "Department", "FirstSeenAt", "ID", "Location", "PublishedAt", "Title", "WorkplaceType"}
	if diff := cmp.Diff(wantPosting, keys(object(t, candidate["Posting"]))); diff != "" {
		t.Errorf("Posting keys (-want +got):\n%s", diff)
	}
}

func TestStagePrepare_JSONShape(t *testing.T) {
	t.Parallel()
	srv, s, _ := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	p := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	cs := connectClient(t, srv)

	got := callTool[map[string]json.RawMessage](t, cs, "stage_prepare", map[string]any{"PostingID": p.ID})
	// ApplicationFormError is omitted when empty.
	wantTop := []string{"ApplicationForm", "ApplicationID", "ApplicationNotes", "CompanyName", "Documents", "LatestReviews", "Posting"}
	if diff := cmp.Diff(wantTop, keys(got)); diff != "" {
		t.Errorf("top-level keys (-want +got):\n%s", diff)
	}
	documents := object(t, got["Documents"])
	var wantTypes []string
	for _, documentType := range document.Types() {
		wantTypes = append(wantTypes, documentType.String())
	}
	sort.Strings(wantTypes)
	if diff := cmp.Diff(wantTypes, keys(documents)); diff != "" {
		t.Errorf("Documents keys (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Exists", "Path", "Version"}, keys(object(t, documents[document.CoverLetter.String()]))); diff != "" {
		t.Errorf("Documents[cover_letter] keys (-want +got):\n%s", diff)
	}
	// Everything needed to draft, but not RawPayload or DescriptionHTML:
	// drafting reads DescriptionText, and the other two were most of the
	// response.
	wantPosting := []string{
		"ApplicationURL", "CompanyID", "CreatedAt", "Department", "DescriptionText",
		"EmploymentType", "FirstSeenAt", "ID", "JobURL", "LastSeenAt", "ListingStatus",
		"Location", "PublishedAt", "Source", "SourceID", "Team", "Title", "UpdatedAt", "WorkplaceType",
	}
	if diff := cmp.Diff(wantPosting, keys(object(t, got["Posting"]))); diff != "" {
		t.Errorf("Posting keys (-want +got):\n%s", diff)
	}
}

// newDraftingServer is a server whose drafting tools answer from drafting.
func newDraftingServer(t *testing.T, drafting fakeDrafting) *mcp.ClientSession {
	t.Helper()
	s := testutil.NewTestApp(t)
	return connectClient(t, New(s.Companies, s.Postings, s.Documents, s.Scores, drafting, nil))
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

var testForm = jobboard.ApplicationForm{
	Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required, document.CoverLetter: jobboard.Optional},
	Questions: []jobboard.Question{{Label: "LinkedIn Profile", Required: true, Type: "text"}},
}

// The form's JSON is part of the agent contract (SKILL.md reads it), so a
// Go-side rename has to fail a test rather than silently change it.
func TestStagePrepare_FormJSON(t *testing.T) {
	t.Parallel()
	cs := newDraftingServer(t, fakeDrafting{prepared: &service.Prepared{Form: service.FormRead{Form: &testForm}}})

	got := callTool[map[string]json.RawMessage](t, cs, "stage_prepare", map[string]any{"PostingID": 7})["ApplicationForm"]
	want := `{"Documents":{"cover_letter":"optional","resume":"required"},"Questions":[{"Label":"LinkedIn Profile","Required":true,"Type":"text"}]}`
	if string(got) != want {
		t.Errorf("ApplicationForm = %s\nwant %s", got, want)
	}
}

func TestStagePrepare_ReturnsWhatDraftingPrepared(t *testing.T) {
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
			cs := newDraftingServer(t, fakeDrafting{prepared: &service.Prepared{
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
			}})

			got := callTool[any](t, cs, "stage_prepare", map[string]any{"PostingID": 7})
			want := stagePrepareOutput{
				Posting:       preparedPosting{ID: 7, CompanyID: 3, Source: jobboard.Greenhouse, SourceID: "job-1", Title: "Engineer", DescriptionText: "Build things"},
				CompanyName:   "Acme",
				ApplicationID: 11,
				Documents: map[document.Type]preparedDocument{
					document.CoverLetter: {Path: "/docs/11/cover_letter.md", Exists: true, Version: "abc"},
					document.Resume:      {Path: "/docs/11/resume.md", Version: service.NoDocumentYet},
				},
				ApplicationNotes:     "referred by a friend",
				LatestReviews:        map[document.Type]latestReview{document.CoverLetter: {Outcome: document.Flagged, Notes: "too generic", Cycle: 2, CreatedAt: reviewedAt}},
				ApplicationForm:      tt.wantForm,
				ApplicationFormError: tt.wantFormError,
			}
			if diff := cmp.Diff(asJSON(t, want), got); diff != "" {
				t.Errorf("stage_prepare (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListPostings_ReturnsTheDraftingQueue(t *testing.T) {
	t.Parallel()
	reviewedAt := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	firstSeen := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	cs := newDraftingServer(t, fakeDrafting{queue: []service.QueuedPosting{
		{
			Posting:     posting.Posting{ID: 7, CompanyID: 3, IngestedFields: posting.IngestedFields{Title: "Engineer", Location: "Remote", DescriptionText: "Build things"}, FirstSeenAt: firstSeen},
			CompanyName: "Acme",
		},
		{
			Posting:     posting.Posting{ID: 8, CompanyID: 4, IngestedFields: posting.IngestedFields{Title: "Designer"}, FirstSeenAt: firstSeen},
			CompanyName: "Globex",
			Application: &application.Application{ID: 11, PostingID: 8, Status: application.Started, Notes: "referred by a friend"},
			LatestReviews: map[document.Type]document.Review{
				document.Resume: {ID: 5, ApplicationID: 11, DocumentType: document.Resume, Cycle: 2, Outcome: document.Flagged, Notes: "too long", CreatedAt: reviewedAt},
			},
		},
	}})

	got := callTool[map[string]any](t, cs, "list_postings", map[string]any{})["Postings"]
	applicationID, status := int64(11), application.Started
	want := []candidate{
		{
			Posting:       postingSummary{ID: 7, Title: "Engineer", Location: "Remote", FirstSeenAt: firstSeen},
			CompanyName:   "Acme",
			LatestReviews: map[document.Type]latestReview{},
		},
		{
			Posting:           postingSummary{ID: 8, Title: "Designer", FirstSeenAt: firstSeen},
			CompanyName:       "Globex",
			ApplicationID:     &applicationID,
			ApplicationStatus: &status,
			ApplicationNotes:  "referred by a friend",
			LatestReviews: map[document.Type]latestReview{
				document.Resume: {Outcome: document.Flagged, Notes: "too long", Cycle: 2, CreatedAt: reviewedAt},
			},
		},
	}
	if diff := cmp.Diff(asJSON(t, want), got); diff != "" {
		t.Errorf("list_postings (-want +got):\n%s", diff)
	}
}

// asJSON is v as the agent reads it: marshalled and decoded generically, so
// types that only marshal compare by their wire form.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return out
}
