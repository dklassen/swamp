package scoring_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/companyfilter"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/postingmarkup"
	"github.com/dklassen/swamp/scoring"
	"github.com/dklassen/swamp/store"
)

func newTestStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	sqlDB, err := store.Open(t.TempDir()+"/test.db", store.DefaultConfig())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return store.New(sqlDB), sqlDB
}

func mustCreateCompany(t *testing.T, s *store.Store, name string) store.Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, "ashby", name)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func mustUpsertPosting(t *testing.T, s *store.Store, companyID int64, sourceID, title, department string) store.Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{
		CompanyID: companyID, Source: "ashby", SourceID: sourceID,
		IngestedFields: store.IngestedFields{Title: title, Department: department, RawPayload: "{}"},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

// mustInsertScore records a score directly, for a fixture's history.
func mustInsertScore(t *testing.T, sqlDB *sql.DB, postingID int64, contentSHA256, profileSHA256 string) {
	t.Helper()
	if _, err := sqlDB.Exec(`INSERT INTO posting_scores (posting_id, score, reason, content_sha256, profile_sha256, model) VALUES (?, 3, 'fixture', ?, ?, 'model')`, postingID, contentSHA256, profileSHA256); err != nil {
		t.Fatalf("insert score: %v", err)
	}
}

func record(ctx context.Context, s *store.Store, provenance scoring.Provenance, entries []scoring.Entry) (scoring.RecordResult, error) {
	var result scoring.RecordResult
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		result, err = scoring.Record(ctx, tx, provenance, entries)
		return err
	})
	return result, err
}

func scoreCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM posting_scores`).Scan(&n); err != nil {
		t.Fatalf("count scores: %v", err)
	}
	return n
}

// pendingReasons is every pending posting, by ID, with why.
func pendingReasons(t *testing.T, s *store.Store, rescoreProfile string) (map[int64]scoring.PendingReason, int) {
	t.Helper()
	page, err := scoring.Pending(context.Background(), s.DB(), rescoreProfile, 0, 100)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	got := map[int64]scoring.PendingReason{}
	for _, p := range page.Postings {
		got[p.ID] = p.Reason
	}
	return got, page.Total
}

func TestPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, sqlDB := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme")
	globex := mustCreateCompany(t, s, "Globex")
	gone := mustCreateCompany(t, s, "Gone")
	if err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		return companyfilter.Replace(ctx, tx, globex.ID, []filter.Filter{{Field: filter.FieldDepartment, Value: "Engineering"}})
	}); err != nil {
		t.Fatalf("companyfilter.Replace: %v", err)
	}

	fresh := mustUpsertPosting(t, s, acme.ID, "1", "Fresh", "")
	changed := mustUpsertPosting(t, s, acme.ID, "2", "Changed", "")
	current := mustUpsertPosting(t, s, acme.ID, "3", "Current", "")
	shown := mustUpsertPosting(t, s, globex.ID, "4", "Shown by the filter", "Engineering")
	mustUpsertPosting(t, s, globex.ID, "5", "Hidden by the filter", "Sales")
	closed := mustUpsertPosting(t, s, acme.ID, "6", "Closed", "")
	archived := mustUpsertPosting(t, s, acme.ID, "7", "Archived", "")
	interested := mustUpsertPosting(t, s, acme.ID, "8", "Interested", "")
	applied := mustUpsertPosting(t, s, acme.ID, "9", "Applied", "")
	mustUpsertPosting(t, s, gone.ID, "10", "At a deleted company", "")
	if err := s.MarkPostingClosed(ctx, closed.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if _, err := postingmarkup.SetArchived(ctx, s.DB(), archived.ID); err != nil {
		t.Fatalf("postingmarkup.SetArchived: %v", err)
	}
	if _, err := postingmarkup.SetInterested(ctx, s.DB(), interested.ID); err != nil {
		t.Fatalf("postingmarkup.SetInterested: %v", err)
	}
	if _, err := createApplication(ctx, s, applied.ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	if err := s.SoftDeleteCompany(ctx, gone.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}

	mustInsertScore(t, sqlDB, changed.ID, changed.ContentSHA256, "old profile")
	mustInsertScore(t, sqlDB, current.ID, current.ContentSHA256, "old profile")
	if _, err := s.UpsertPosting(ctx, store.CreatePostingParams{CompanyID: acme.ID, Source: "ashby", SourceID: "2",
		IngestedFields: store.IngestedFields{Title: "Changed", DescriptionText: "Rewritten.", RawPayload: "{}"}}); err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	tests := []struct {
		name           string
		rescoreProfile string
		want           map[int64]scoring.PendingReason
	}{
		{"default: no score on the current content", "", map[int64]scoring.PendingReason{fresh.ID: scoring.PendingNew, changed.ID: scoring.PendingChanged, shown.ID: scoring.PendingNew}},
		{"rescore against a new profile", "new profile", map[int64]scoring.PendingReason{fresh.ID: scoring.PendingNew, changed.ID: scoring.PendingChanged, current.ID: scoring.PendingRescore, shown.ID: scoring.PendingNew}},
		{"rescore against the profile already used", "old profile", map[int64]scoring.PendingReason{fresh.ID: scoring.PendingNew, changed.ID: scoring.PendingChanged, shown.ID: scoring.PendingNew}},
	}
	for _, tt := range tests {
		got, total := pendingReasons(t, s, tt.rescoreProfile)
		if diff := cmp.Diff(tt.want, got); diff != "" {
			t.Errorf("%s: pending postings and why (-want +got):\n%s", tt.name, diff)
		}
		if total != len(tt.want) {
			t.Errorf("%s: Total = %d, want %d", tt.name, total, len(tt.want))
		}
	}
}

// A rescore needs no session to resume: what it has done has scores
// against the new profile.
func TestPending_ARescoreResumes(t *testing.T) {
	t.Parallel()
	s, sqlDB := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme")
	done := mustUpsertPosting(t, s, acme.ID, "1", "Rescored", "")
	left := mustUpsertPosting(t, s, acme.ID, "2", "Not yet", "")
	mustInsertScore(t, sqlDB, done.ID, done.ContentSHA256, "old profile")
	mustInsertScore(t, sqlDB, left.ID, left.ContentSHA256, "old profile")

	mustInsertScore(t, sqlDB, done.ID, done.ContentSHA256, "new profile")

	got, _ := pendingReasons(t, s, "new profile")
	if diff := cmp.Diff(map[int64]scoring.PendingReason{left.ID: scoring.PendingRescore}, got); diff != "" {
		t.Errorf("rescore pending (-want +got):\n%s", diff)
	}
}

func TestPending_Pages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme")
	var ids []int64
	for i := 1; i <= 5; i++ {
		ids = append(ids, mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i), "").ID)
	}

	var got [][]int64
	var hasMore []bool
	var before int64
	for range 3 {
		page, err := scoring.Pending(ctx, s.DB(), "", before, 2)
		if err != nil {
			t.Fatalf("Pending: %v", err)
		}
		if page.Total != 5 {
			t.Errorf("Total = %d, want 5", page.Total)
		}
		var pageIDs []int64
		for _, p := range page.Postings {
			pageIDs = append(pageIDs, p.ID)
		}
		got = append(got, pageIDs)
		hasMore = append(hasMore, page.HasMore)
		before = pageIDs[len(pageIDs)-1]
	}
	if diff := cmp.Diff([][]int64{{ids[4], ids[3]}, {ids[2], ids[1]}, {ids[0]}}, got); diff != "" {
		t.Errorf("pages (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]bool{true, true, false}, hasMore); diff != "" {
		t.Errorf("HasMore (-want +got):\n%s", diff)
	}
}

func TestRecord_RecordsValidEntriesAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, sqlDB := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme")
	good := mustUpsertPosting(t, s, acme.ID, "1", "Good", "")
	stale := mustUpsertPosting(t, s, acme.ID, "2", "Stale", "")
	other := mustUpsertPosting(t, s, acme.ID, "3", "Other", "")
	repeat := mustUpsertPosting(t, s, acme.ID, "4", "Already scored", "")
	provenance := scoring.Provenance{Model: "claude-opus-5-5", ClientName: "claude-code", ClientVersion: "2.4.1", ProfileSHA256: "profile"}
	if _, err := record(ctx, s, provenance, []scoring.Entry{{PostingID: repeat.ID, Score: 2, Reason: "first", ContentSHA256: repeat.ContentSHA256}}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	result, err := record(ctx, s, provenance, []scoring.Entry{
		{PostingID: good.ID, Score: 4, Reason: "Go, remote", ContentSHA256: good.ContentSHA256},
		{PostingID: stale.ID, Score: 3, Reason: "ok", ContentSHA256: "an older hash"},
		{PostingID: other.ID, Score: 6, Reason: "too high", ContentSHA256: other.ContentSHA256},
		{PostingID: other.ID, Score: 0, Reason: "too low", ContentSHA256: other.ContentSHA256},
		{PostingID: other.ID, Score: 2, Reason: "", ContentSHA256: other.ContentSHA256},
		{PostingID: 999, Score: 2, Reason: "no such posting", ContentSHA256: "x"},
		{PostingID: repeat.ID, Score: 2, Reason: "again", ContentSHA256: repeat.ContentSHA256},
		{PostingID: good.ID, Score: 1, Reason: "twice in one batch", ContentSHA256: good.ContentSHA256},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	if diff := cmp.Diff([]int64{good.ID}, result.Recorded); diff != "" {
		t.Errorf("recorded (-want +got):\n%s", diff)
	}
	want := []scoring.Refused{
		{PostingID: stale.ID, Why: scoring.ContentChanged},
		{PostingID: other.ID, Why: scoring.OutOfRange},
		{PostingID: other.ID, Why: scoring.OutOfRange},
		{PostingID: other.ID, Why: scoring.NoReason},
		{PostingID: 999, Why: scoring.NoSuchPosting},
		{PostingID: repeat.ID, Why: scoring.Repeat},
		{PostingID: good.ID, Why: scoring.Repeat},
	}
	if diff := cmp.Diff(want, result.Refused); diff != "" {
		t.Errorf("refused (-want +got):\n%s", diff)
	}

	var model, client, version, profile string
	if err := sqlDB.QueryRow(`SELECT model, client_name, client_version, profile_sha256 FROM posting_scores WHERE posting_id = ?`, good.ID).Scan(&model, &client, &version, &profile); err != nil {
		t.Fatalf("read score: %v", err)
	}
	if got := (scoring.Provenance{Model: model, ClientName: client, ClientVersion: version, ProfileSHA256: profile}); got != provenance {
		t.Errorf("stored provenance %+v, want %+v", got, provenance)
	}
}

// The same content scored by another model, or against another profile,
// isn't a repeat.
func TestRecord_AnotherModelOrProfileIsntARepeat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme")
	p := mustUpsertPosting(t, s, acme.ID, "1", "Role", "")
	entry := []scoring.Entry{{PostingID: p.ID, Score: 3, Reason: "ok", ContentSHA256: p.ContentSHA256}}

	for _, provenance := range []scoring.Provenance{
		{Model: "claude-opus-5-5", ProfileSHA256: "profile"},
		{Model: "claude-sonnet-5", ProfileSHA256: "profile"},
		{Model: "claude-opus-5-5", ProfileSHA256: "edited profile"},
	} {
		result, err := record(ctx, s, provenance, entry)
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if len(result.Recorded) != 1 {
			t.Errorf("%+v: refused %+v, want it recorded", provenance, result.Refused)
		}
	}
}

func TestRecord_RefusesACallWithNoModelOrProfile(t *testing.T) {
	t.Parallel()
	s, sqlDB := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme")
	p := mustUpsertPosting(t, s, acme.ID, "1", "Role", "")
	entry := []scoring.Entry{{PostingID: p.ID, Score: 3, Reason: "ok", ContentSHA256: p.ContentSHA256}}

	for _, provenance := range []scoring.Provenance{{ProfileSHA256: "profile"}, {Model: "claude-opus-5-5"}} {
		if _, err := record(context.Background(), s, provenance, entry); err == nil {
			t.Errorf("%+v: Record succeeded, want it refused", provenance)
		}
	}
	if n := scoreCount(t, sqlDB); n != 0 {
		t.Errorf("%d scores stored, want 0", n)
	}
}

// The trigger makes the second insert fail after the first has run; the
// caller then commits anyway, which must not keep the first.
func TestRecord_FailingWriteInACallersTransaction_RecordsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, sqlDB := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme")
	first := mustUpsertPosting(t, s, acme.ID, "1", "First", "")
	second := mustUpsertPosting(t, s, acme.ID, "2", "Second", "")
	if _, err := sqlDB.Exec(`CREATE TRIGGER fail_score BEFORE INSERT ON posting_scores WHEN NEW.reason = 'fails' BEGIN SELECT RAISE(ABORT, 'fails'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	var recordErr error
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		_, recordErr = scoring.Record(ctx, tx, scoring.Provenance{Model: "m", ProfileSHA256: "p"}, []scoring.Entry{
			{PostingID: first.ID, Score: 4, Reason: "ok", ContentSHA256: first.ContentSHA256},
			{PostingID: second.ID, Score: 4, Reason: "fails", ContentSHA256: second.ContentSHA256},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if recordErr == nil {
		t.Fatal("Record with a failing insert: err = nil, want an error")
	}
	if n := scoreCount(t, sqlDB); n != 0 {
		t.Errorf("%d scores stored after a failed Record, want 0", n)
	}
}

func TestForPosting_NewestFirstWithWhatProducedThem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme")
	p := mustUpsertPosting(t, s, acme.ID, "1", "Role", "")
	other := mustUpsertPosting(t, s, acme.ID, "2", "Other", "")
	first := scoring.Provenance{Model: "claude-sonnet-5", ClientName: "claude-code", ClientVersion: "2.4.0", ProfileSHA256: "profile"}
	second := scoring.Provenance{Model: "claude-opus-5-5", ClientName: "claude-code", ClientVersion: "2.4.1", ProfileSHA256: "edited profile"}
	for _, r := range []struct {
		provenance scoring.Provenance
		entries    []scoring.Entry
	}{
		{first, []scoring.Entry{{PostingID: p.ID, Score: 2, Reason: "first look", ContentSHA256: p.ContentSHA256}, {PostingID: other.ID, Score: 1, Reason: "not this one", ContentSHA256: other.ContentSHA256}}},
		{second, []scoring.Entry{{PostingID: p.ID, Score: 4, Reason: "after the edit", ContentSHA256: p.ContentSHA256}}},
	} {
		if _, err := record(ctx, s, r.provenance, r.entries); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	scores, err := scoring.ForPosting(ctx, s.DB(), p.ID)
	if err != nil {
		t.Fatalf("ForPosting: %v", err)
	}
	type summary struct {
		Score      int
		Reason     string
		Provenance scoring.Provenance
		Content    string
	}
	var got []summary
	for _, sc := range scores {
		if sc.ScoredAt.IsZero() {
			t.Errorf("score %d has no ScoredAt", sc.ID)
		}
		got = append(got, summary{sc.Score, sc.Reason, sc.Provenance, sc.ContentSHA256})
	}
	want := []summary{{4, "after the edit", second, p.ContentSHA256}, {2, "first look", first, p.ContentSHA256}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("scores (-want +got):\n%s", diff)
	}
}

// The vocabularies the agent reads go to and from JSON as their names.
func TestVocabularies_RoundTripThroughJSONByName(t *testing.T) {
	t.Parallel()

	for _, r := range []scoring.PendingReason{scoring.PendingNew, scoring.PendingChanged, scoring.PendingRescore} {
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", r, err)
		}
		var decoded scoring.PendingReason
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != r || string(encoded) != `"`+r.String()+`"` {
			t.Errorf("%v: encoded %s, decoded %v (%v)", r, encoded, decoded, err)
		}
	}
	for _, r := range scoring.Refusals() {
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", r, err)
		}
		var decoded scoring.Refusal
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != r || string(encoded) != `"`+r.String()+`"` {
			t.Errorf("%v: encoded %s, decoded %v (%v)", r, encoded, decoded, err)
		}
	}
	var reason scoring.PendingReason
	if err := json.Unmarshal([]byte(`"stale"`), &reason); err == nil {
		t.Error(`decoding the pending reason "stale" succeeded, want it refused`)
	}
	var refusal scoring.Refusal
	if err := json.Unmarshal([]byte(`"too_high"`), &refusal); err == nil {
		t.Error(`decoding the refusal "too_high" succeeded, want it refused`)
	}
}

func createApplication(ctx context.Context, s *store.Store, postingID int64) (application.Application, error) {
	var app application.Application
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		app, err = application.Create(ctx, tx, postingID)
		return err
	})
	return app, err
}
