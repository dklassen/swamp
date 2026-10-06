package migrations

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// openEmpty is a database with no migrations applied, as on first run.
func openEmpty(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	return sqlDB
}

// TestCheck_DatabaseBehind_SaysToRunMigrate: the TUI, mcp-serve and fetch
// no longer migrate when they start (#273), so one started against an
// older database must refuse and say what to do.
func TestCheck_DatabaseBehind_SaysToRunMigrate(t *testing.T) {
	t.Parallel()

	err := Check(context.Background(), openEmpty(t))

	var behind *BehindError
	if !errors.As(err, &behind) {
		t.Fatalf("Check on an unmigrated database = %v, want a *BehindError", err)
	}
	if behind.Current != 0 || behind.Latest != Latest() {
		t.Errorf("BehindError = %+v, want Current 0, Latest %d", behind, Latest())
	}
	if !strings.Contains(err.Error(), "swamp migrate") {
		t.Errorf("error %q doesn't say to run swamp migrate", err)
	}
}

// TestUp_MigratesToLatestAndIsANoOpAfter: `swamp migrate` on a new or
// older database brings it to Latest, and running it again changes
// nothing.
func TestUp_MigratesToLatestAndIsANoOpAfter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB := openEmpty(t)

	applied, err := Up(ctx, sqlDB)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if want := int(Latest()); len(applied) != want {
		t.Errorf("Up applied %d migrations, want %d", len(applied), want)
	}
	if err := Check(ctx, sqlDB); err != nil {
		t.Errorf("Check after Up = %v, want nil", err)
	}

	applied, err = Up(ctx, sqlDB)
	if err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("second Up applied %v, want nothing", applied)
	}
}

// TestCheck_DatabaseAhead_Refuses: an older binary run after a newer one
// migrated the database would be running old code on a schema it doesn't
// know, so it refuses too.
func TestCheck_DatabaseAhead_Refuses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB := openEmpty(t)
	if _, err := Up(ctx, sqlDB); err != nil {
		t.Fatalf("Up: %v", err)
	}
	// As a newer binary's migration would record itself.
	if _, err := sqlDB.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, Latest()+1); err != nil {
		t.Fatalf("record a newer version: %v", err)
	}

	err := Check(ctx, sqlDB)

	var ahead *AheadError
	if !errors.As(err, &ahead) {
		t.Fatalf("Check on a newer database = %v, want an *AheadError", err)
	}
	if ahead.Current != Latest()+1 || ahead.Latest != Latest() {
		t.Errorf("AheadError = %+v, want Current %d, Latest %d", ahead, Latest()+1, Latest())
	}
}

// TestCheck_ChangesNothing: every process checks as it starts, so a check
// mustn't write, not even goose's own version table.
func TestCheck_ChangesNothing(t *testing.T) {
	t.Parallel()

	sqlDB := openEmpty(t)
	_ = Check(context.Background(), sqlDB)

	var tables int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&tables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 0 {
		t.Errorf("Check on an empty database left %d schema objects, want 0", tables)
	}
}

// TestCheck_AfterARollback_AgreesWithGoose: Check reads the version itself
// (to avoid goose creating its table), so it must agree with goose after
// goose rolls a migration back.
func TestCheck_AfterARollback_AgreesWithGoose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB := openEmpty(t)
	if _, err := Up(ctx, sqlDB); err != nil {
		t.Fatalf("Up: %v", err)
	}
	p, err := provider(sqlDB)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if _, err := p.Down(ctx); err != nil {
		t.Fatalf("Down: %v", err)
	}
	gooseVersion, err := p.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	}

	err = Check(ctx, sqlDB)

	var behind *BehindError
	if !errors.As(err, &behind) || behind.Current != gooseVersion || gooseVersion != Latest()-1 {
		t.Errorf("Check after one rollback = %v; goose says version %d; want a *BehindError at %d", err, gooseVersion, Latest()-1)
	}
}
