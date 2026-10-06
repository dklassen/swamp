package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Applying migrations is an explicit step, `swamp migrate`, not something
// every process does as it starts (#273). The TUI, mcp-serve and fetch
// only Check, so an upgrade happens once, deliberately, and never under a
// process still running the previous binary.

// BehindError is Check's error for a database older than this binary.
type BehindError struct {
	Current, Latest int64
}

func (e *BehindError) Error() string {
	return fmt.Sprintf("the database is at version %d; this binary needs %d: run `swamp migrate` (back up the database first)", e.Current, e.Latest)
}

// AheadError is Check's error for a database migrated by a newer binary.
type AheadError struct {
	Current, Latest int64
}

func (e *AheadError) Error() string {
	return fmt.Sprintf("the database is at version %d, newer than this binary's %d: use the newer binary", e.Current, e.Latest)
}

// Latest is the version of the newest embedded migration.
func Latest() int64 {
	names, err := fs.Glob(FS, "*.sql")
	if err != nil {
		panic(fmt.Sprintf("migrations: list embedded files: %v", err))
	}
	var latest int64
	for _, name := range names {
		version, err := goose.NumericComponent(name)
		if err != nil {
			panic(fmt.Sprintf("migrations: %s: %v", name, err))
		}
		latest = max(latest, version)
	}
	return latest
}

// Check reports whether db is at exactly Latest: a *BehindError or an
// *AheadError if not. It only reads, so it can run every time a process
// starts.
func Check(ctx context.Context, db *sql.DB) error {
	current, err := version(ctx, db)
	if err != nil {
		return err
	}
	switch latest := Latest(); {
	case current < latest:
		return &BehindError{Current: current, Latest: latest}
	case current > latest:
		return &AheadError{Current: current, Latest: latest}
	}
	return nil
}

// version is the database's migration version, read the way goose's own
// GetDBVersion reads it (rolling back deletes a version's row, so the
// highest row is current), but without goose's side effect of creating
// its version table: an unmigrated database is version 0.
func version(ctx context.Context, db *sql.DB) (int64, error) {
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'goose_db_version'`).Scan(&tables); err != nil {
		return 0, fmt.Errorf("migrations: read the database's version: %w", err)
	}
	if tables == 0 {
		return 0, nil
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("migrations: read the database's version: %w", err)
	}
	return v.Int64, nil
}

// Up applies every embedded migration db doesn't have yet and returns
// their versions, in order; none if it was already at Latest.
func Up(ctx context.Context, db *sql.DB) ([]int64, error) {
	p, err := provider(db)
	if err != nil {
		return nil, err
	}
	results, err := p.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrations: apply: %w", err)
	}
	applied := make([]int64, len(results))
	for i, r := range results {
		applied[i] = r.Source.Version
	}
	return applied, nil
}

// provider is a goose provider for db and the embedded migrations. Its
// Close would close db, which belongs to the caller, so it's never
// called.
func provider(db *sql.DB) (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db, FS)
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return p, nil
}
