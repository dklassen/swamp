package store

import (
	"context"
	"testing"
)

func TestListActiveApplications_ExcludesRejectedAndOfferDeclined(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	started := mustUpsertPosting(t, s, acme.ID, "job-started", "Started Role")
	mustCreateApplication(t, s, started.ID)

	interviewing := mustUpsertPosting(t, s, acme.ID, "job-interviewing", "Interviewing Role")
	mustCreateApplication(t, s, interviewing.ID)
	if _, err := s.UpdateApplicationStatus(ctx, interviewing.ID, ApplicationStatusInterviewing); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	rejected := mustUpsertPosting(t, s, acme.ID, "job-rejected", "Rejected Role")
	mustCreateApplication(t, s, rejected.ID)
	if _, err := s.UpdateApplicationStatus(ctx, rejected.ID, ApplicationStatusRejected); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	declined := mustUpsertPosting(t, s, acme.ID, "job-declined", "Declined Role")
	mustCreateApplication(t, s, declined.ID)
	if _, err := s.UpdateApplicationStatus(ctx, declined.ID, ApplicationStatusOfferDeclined); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	noApplication := mustUpsertPosting(t, s, acme.ID, "job-no-app", "No Application Role")
	_ = noApplication

	got, err := s.ListActiveApplications(ctx)
	if err != nil {
		t.Fatalf("ListActiveApplications: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (started + interviewing only): %+v", len(got), got)
	}
	gotTitles := map[string]bool{}
	for _, v := range got {
		gotTitles[v.Posting.Title] = true
	}
	if !gotTitles["Started Role"] || !gotTitles["Interviewing Role"] {
		t.Fatalf("got titles = %v, want Started Role and Interviewing Role present", gotTitles)
	}
	if gotTitles["Rejected Role"] || gotTitles["Declined Role"] || gotTitles["No Application Role"] {
		t.Fatalf("got titles = %v, want rejected/declined/no-application excluded", gotTitles)
	}
}

// TestListActiveApplications_MatchesTerminalApplicationStatuses is a
// drift-detection test, not just a regression test: it derives "which
// statuses should be excluded" from TerminalApplicationStatuses() itself
// rather than hardcoding rejected/offer_declined a second time, so it
// stays correct (and would catch a real mismatch) even if the set of
// terminal statuses changes later (see decisions.log, issue #60).
func TestListActiveApplications_MatchesTerminalApplicationStatuses(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	terminal := map[ApplicationStatus]bool{}
	for _, status := range TerminalApplicationStatuses() {
		terminal[status] = true
	}

	wantIncluded := map[string]bool{}
	for _, status := range ApplicationStatuses() {
		posting := mustUpsertPosting(t, s, acme.ID, "job-"+status.String(), status.String())
		mustCreateApplication(t, s, posting.ID)
		if _, err := s.UpdateApplicationStatus(ctx, posting.ID, status); err != nil {
			t.Fatalf("UpdateApplicationStatus(%s): %v", status, err)
		}
		if !terminal[status] {
			wantIncluded[posting.Title] = true
		}
	}

	got, err := s.ListActiveApplications(ctx)
	if err != nil {
		t.Fatalf("ListActiveApplications: %v", err)
	}

	gotTitles := map[string]bool{}
	for _, v := range got {
		gotTitles[v.Posting.Title] = true
	}
	if len(gotTitles) != len(wantIncluded) {
		t.Fatalf("ListActiveApplications returned %v, want exactly %v", gotTitles, wantIncluded)
	}
	for title := range wantIncluded {
		if !gotTitles[title] {
			t.Fatalf("ListActiveApplications missing %q (non-terminal status), got %v", title, gotTitles)
		}
	}
}

func TestListActiveApplications_IncludesCompanyNameAndApplicationFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)

	got, err := s.ListActiveApplications(ctx)
	if err != nil {
		t.Fatalf("ListActiveApplications: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}

	v := got[0]
	if v.CompanyName != "Acme" {
		t.Fatalf("CompanyName = %q, want %q", v.CompanyName, "Acme")
	}
	if v.Posting.Title != "Software Engineer" {
		t.Fatalf("Posting.Title = %q, want %q", v.Posting.Title, "Software Engineer")
	}
	if v.ID != app.ID {
		t.Fatalf("ID = %d, want %d", v.ID, app.ID)
	}
	if v.Status != ApplicationStatusStarted {
		t.Fatalf("Status = %s, want %s", v.Status, ApplicationStatusStarted)
	}
	if v.Notes != app.Notes {
		t.Fatalf("Notes = %q, want %q", v.Notes, app.Notes)
	}
	if v.PostingID != posting.ID {
		t.Fatalf("PostingID = %d, want %d", v.PostingID, posting.ID)
	}
}

func TestListActiveApplications_NoReviews_LatestReviewsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)

	got, err := s.ListActiveApplications(ctx)
	if err != nil {
		t.Fatalf("ListActiveApplications: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].LatestReviews) != 0 {
		t.Fatalf("LatestReviews = %+v, want empty (no reviews recorded)", got[0].LatestReviews)
	}
}

func TestListActiveApplications_IncludesLatestDocumentReviews(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)

	if _, err := s.CreateDocumentReview(ctx, app.ID, DocumentTypeCoverLetter, "draft one", ReviewOutcomeFlagged, "too generic"); err != nil {
		t.Fatalf("CreateDocumentReview (first): %v", err)
	}
	second, err := s.CreateDocumentReview(ctx, app.ID, DocumentTypeCoverLetter, "draft two", ReviewOutcomePassed, "")
	if err != nil {
		t.Fatalf("CreateDocumentReview (second): %v", err)
	}

	got, err := s.ListActiveApplications(ctx)
	if err != nil {
		t.Fatalf("ListActiveApplications: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}

	review, ok := got[0].LatestReviews[DocumentTypeCoverLetter]
	if !ok {
		t.Fatalf("LatestReviews[%q] missing, want the most recent review present", DocumentTypeCoverLetter)
	}
	if review.ID != second.ID {
		t.Fatalf("LatestReviews[%q].ID = %d, want %d (most recent cycle, not the first)", DocumentTypeCoverLetter, review.ID, second.ID)
	}
	if _, ok := got[0].LatestReviews[DocumentTypeResume]; ok {
		t.Fatalf("LatestReviews[%q] present, want absent (no resume review recorded)", DocumentTypeResume)
	}
}
