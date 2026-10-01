package store

import (
	"context"
	"testing"
	"time"
)

func TestApplicationForm_SaveThenGet(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "greenhouse", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")

	if _, _, ok, err := s.GetApplicationForm(ctx, posting.ID); err != nil || ok {
		t.Fatalf("GetApplicationForm before saving = ok %v, err %v; want none", ok, err)
	}

	if err := s.SaveApplicationForm(ctx, posting.ID, `{"v":1}`); err != nil {
		t.Fatalf("SaveApplicationForm: %v", err)
	}
	form, fetchedAt, ok, err := s.GetApplicationForm(ctx, posting.ID)
	if err != nil || !ok {
		t.Fatalf("GetApplicationForm = ok %v, err %v; want the saved form", ok, err)
	}
	if form != `{"v":1}` {
		t.Errorf("form = %s, want {\"v\":1}", form)
	}
	if time.Since(fetchedAt) > time.Minute || fetchedAt.Location() != time.UTC {
		t.Errorf("fetchedAt = %v, want about now, in UTC", fetchedAt)
	}

	// Saving again replaces the form: a re-fetch, not a second copy.
	if _, err := s.sqlDB.ExecContext(ctx, `UPDATE posting_application_forms SET fetched_at = '2026-01-01 00:00:00' WHERE posting_id = ?`, posting.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if err := s.SaveApplicationForm(ctx, posting.ID, `{"v":2}`); err != nil {
		t.Fatalf("second SaveApplicationForm: %v", err)
	}
	form, fetchedAt, _, err = s.GetApplicationForm(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetApplicationForm: %v", err)
	}
	if form != `{"v":2}` || fetchedAt.Year() == 2026 && fetchedAt.Month() == time.January {
		t.Errorf("after re-saving: form %s, fetchedAt %v; want the new form and a new fetchedAt", form, fetchedAt)
	}
}
