package store

import (
	"context"
	"testing"
	"time"

	"github.com/dklassen/swamp/applicationstatus"
)

func TestListActiveApplications_ExcludesRejectedAndOfferDeclined(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	started := mustUpsertPosting(t, s, acme.ID, "job-started", "Started Role")
	mustCreateApplication(t, s, started.ID)

	interviewing := mustUpsertPosting(t, s, acme.ID, "job-interviewing", "Interviewing Role")
	mustCreateApplication(t, s, interviewing.ID)
	if _, err := s.UpdateApplicationStatus(ctx, interviewing.ID, applicationstatus.Interviewing); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	rejected := mustUpsertPosting(t, s, acme.ID, "job-rejected", "Rejected Role")
	mustCreateApplication(t, s, rejected.ID)
	if _, err := s.UpdateApplicationStatus(ctx, rejected.ID, applicationstatus.Rejected); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	declined := mustUpsertPosting(t, s, acme.ID, "job-declined", "Declined Role")
	mustCreateApplication(t, s, declined.ID)
	if _, err := s.UpdateApplicationStatus(ctx, declined.ID, applicationstatus.OfferDeclined); err != nil {
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

// TestListActiveApplications_MatchesTerminalStatuses is a
// drift-detection test, not just a regression test: it derives "which
// statuses should be excluded" from applicationstatus.Terminal() itself
// rather than hardcoding rejected/offer_declined a second time, so it
// stays correct (and would catch a real mismatch) even if the set of
// terminal statuses changes later (issue #60).
func TestListActiveApplications_MatchesTerminalStatuses(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	terminal := map[applicationstatus.Status]bool{}
	for _, status := range applicationstatus.Terminal() {
		terminal[status] = true
	}

	wantIncluded := map[string]bool{}
	for _, status := range applicationstatus.All() {
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
	if v.Status != applicationstatus.Started {
		t.Fatalf("Status = %s, want %s", v.Status, applicationstatus.Started)
	}
	if v.Notes != app.Notes {
		t.Fatalf("Notes = %q, want %q", v.Notes, app.Notes)
	}
	if v.PostingID != posting.ID {
		t.Fatalf("PostingID = %d, want %d", v.PostingID, posting.ID)
	}
}

// StatusSince is when each application entered its current status (#164),
// from status history (#162), so the home screen can show how long it's
// been stuck there. An application with no history falls back to
// CreatedAt.
func TestListActiveApplications_StatusSince(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	withHistory := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application := mustCreateApplication(t, s, withHistory.ID)
	if _, err := s.UpdateApplicationStatus(ctx, withHistory.ID, applicationstatus.Submitted); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}
	startedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	submittedAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if _, err := s.sqlDB.ExecContext(ctx,
		`UPDATE application_status_history SET changed_at = ? WHERE application_id = ? AND status = 'application_submitted'`,
		submittedAt, application.ID); err != nil {
		t.Fatalf("set changed_at: %v", err)
	}
	if _, err := s.sqlDB.ExecContext(ctx,
		`UPDATE application_status_history SET changed_at = ? WHERE application_id = ? AND status = 'application_started'`,
		startedAt, application.ID); err != nil {
		t.Fatalf("set changed_at: %v", err)
	}

	noHistory := mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	bare := mustCreateApplication(t, s, noHistory.ID)
	if _, err := s.sqlDB.ExecContext(ctx, `DELETE FROM application_status_history WHERE application_id = ?`, bare.ID); err != nil {
		t.Fatalf("delete history: %v", err)
	}

	views, err := s.ListActiveApplications(ctx)
	if err != nil {
		t.Fatalf("ListActiveApplications: %v", err)
	}
	got := map[int64]time.Time{}
	for _, v := range views {
		got[v.ID] = v.StatusSince
	}
	if !got[application.ID].Equal(submittedAt) {
		t.Errorf("StatusSince with history = %v, want %v (when it entered application_submitted)", got[application.ID], submittedAt)
	}
	if !got[bare.ID].Equal(bare.CreatedAt) {
		t.Errorf("StatusSince without history = %v, want CreatedAt %v", got[bare.ID], bare.CreatedAt)
	}
}
