package migrations

import (
	"database/sql"
	"testing"

	"github.com/dklassen/swamp/store"
)

// migrated is a database at Latest.
func migrated(t *testing.T) *sql.DB {
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
	if _, err := Up(t.Context(), sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return sqlDB
}
