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
	for _, a := range got {
		gotTitles[a.Posting.Title] = true
	}
	if !gotTitles["Started Role"] || !gotTitles["Interviewing Role"] {
		t.Fatalf("got titles = %v, want Started Role and Interviewing Role present", gotTitles)
	}
	if gotTitles["Rejected Role"] || gotTitles["Declined Role"] || gotTitles["No Application Role"] {
		t.Fatalf("got titles = %v, want rejected/declined/no-application excluded", gotTitles)
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

	a := got[0]
	if a.CompanyName != "Acme" {
		t.Fatalf("CompanyName = %q, want %q", a.CompanyName, "Acme")
	}
	if a.Posting.Title != "Software Engineer" {
		t.Fatalf("Posting.Title = %q, want %q", a.Posting.Title, "Software Engineer")
	}
	if a.ApplicationID != app.ID {
		t.Fatalf("ApplicationID = %d, want %d", a.ApplicationID, app.ID)
	}
	if a.ApplicationStatus != ApplicationStatusStarted {
		t.Fatalf("ApplicationStatus = %s, want %s", a.ApplicationStatus, ApplicationStatusStarted)
	}
}
