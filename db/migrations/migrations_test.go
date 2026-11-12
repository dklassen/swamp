package migrations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/dklassen/swamp/store"
)

func migrateTo(t *testing.T, version int64) *sql.DB {
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

	if err := upTo(sqlDB, version); err != nil {
		t.Fatalf("migrate to version %d: %v", version, err)
	}
	return sqlDB
}

// upTo and downTo go through a goose provider, not goose's package
// functions, whose global state parallel tests race on.
func upTo(db *sql.DB, version int64) error {
	p, err := provider(db)
	if err != nil {
		return err
	}
	_, err = p.UpTo(context.Background(), version)
	return err
}

//nolint:unused // for the next migration's Down test
func downTo(db *sql.DB, version int64) error {
	p, err := provider(db)
	if err != nil {
		return err
	}
	_, err = p.DownTo(context.Background(), version)
	return err
}
