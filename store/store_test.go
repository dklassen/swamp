package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/db/migrations"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return New(sqlDB)
}

func mustCreateCompany(t *testing.T, s *Store, name, source, sourceRef string) Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, source, sourceRef)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}
