package companyfilter_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/companyfilter"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/store"
)

func newTestStore(t *testing.T) *store.Store {
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
	return store.New(sqlDB)
}

func newCompany(t *testing.T, s *store.Store) int64 {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), "Acme", "ashby", "acme")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c.ID
}

func replace(ctx context.Context, s *store.Store, companyID int64, filters []filter.Filter) error {
	return store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		return companyfilter.Replace(ctx, tx, companyID, filters)
	})
}

func list(t *testing.T, ctx context.Context, s *store.Store, companyID int64) []filter.Filter {
	t.Helper()
	got, err := companyfilter.List(ctx, s.DB(), companyID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return got
}

func TestReplace_ThenList_ReturnsTheFiltersInOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	id := newCompany(t, s)
	want := []filter.Filter{
		{Field: filter.FieldDepartment, Value: "Engineering"},
		{Field: filter.FieldLocation, Value: "Remote"},
	}

	if err := replace(ctx, s, id, want); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if diff := cmp.Diff(want, list(t, ctx, s, id)); diff != "" {
		t.Errorf("List (-want +got):\n%s", diff)
	}
}

func TestReplace_RemovesThePreviousFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	id := newCompany(t, s)
	if err := replace(ctx, s, id, []filter.Filter{{Field: filter.FieldDepartment, Value: "Sales"}}); err != nil {
		t.Fatalf("first Replace: %v", err)
	}
	want := []filter.Filter{{Field: filter.FieldLocation, Value: "Remote"}}

	if err := replace(ctx, s, id, want); err != nil {
		t.Fatalf("second Replace: %v", err)
	}
	if diff := cmp.Diff(want, list(t, ctx, s, id)); diff != "" {
		t.Errorf("List (-want +got):\n%s", diff)
	}
}

// The CHECK on company_filters.field makes the second insert fail after
// the delete has run.
func TestReplace_FailingInsert_KeepsTheOldFilters(t *testing.T) {
	t.Parallel()
	old := []filter.Filter{{Field: filter.FieldDepartment, Value: "Sales"}}
	bad := []filter.Filter{{Field: filter.FieldLocation, Value: "Remote"}, {Field: "unsupported", Value: "x"}}
	for _, tc := range []struct {
		name string
		run  func(ctx context.Context, s *store.Store, id int64) error
	}{
		{"in its own transaction", func(ctx context.Context, s *store.Store, id int64) error {
			return replace(ctx, s, id, bad)
		}},
		{"in a caller's transaction that commits anyway", func(ctx context.Context, s *store.Store, id int64) error {
			var replaceErr error
			err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
				replaceErr = companyfilter.Replace(ctx, tx, id, bad)
				return nil
			})
			if err != nil {
				return err
			}
			return replaceErr
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := newTestStore(t)
			id := newCompany(t, s)
			if err := replace(ctx, s, id, old); err != nil {
				t.Fatalf("first Replace: %v", err)
			}

			if err := tc.run(ctx, s, id); err == nil {
				t.Fatal("Replace with an unsupported field: err = nil, want an error")
			}
			if diff := cmp.Diff(old, list(t, ctx, s, id)); diff != "" {
				t.Errorf("List after a failed Replace (-want +got):\n%s", diff)
			}
		})
	}
}
