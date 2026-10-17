package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

var errFail = errors.New("fail")

func createCompany(ctx context.Context, r store.Runner, name string) error {
	return store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		_, err := q.CreateCompany(ctx, sqlcdb.CreateCompanyParams{Name: name, Source: jobboard.Ashby, SourceRef: name})
		return err
	})
}

func companyNames(t *testing.T, ctx context.Context, r store.Runner) []string {
	t.Helper()
	companies, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.Company, error) {
		return q.ListActiveCompanies(ctx)
	})
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	names := make([]string, len(companies))
	for i, c := range companies {
		names[i] = c.Name
	}
	return names
}

func TestInTx_FnFails_RollsBackItsWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestStore(t)

	err := store.InTx(ctx, s, func(tx store.Tx) error {
		if err := createCompany(ctx, tx, "a"); err != nil {
			return err
		}
		return errFail
	})
	if !errors.Is(err, errFail) {
		t.Fatalf("InTx: err = %v, want errFail", err)
	}
	if got := companyNames(t, ctx, s); len(got) != 0 {
		t.Errorf("companies after a failed InTx = %q, want none", got)
	}
}

// The outer fn carrying on after the inner failure is what a joined
// transaction would get wrong: the inner write would commit with it.
func TestInTx_OnTx_FailingFnUndoesOnlyItsOwnWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestStore(t)

	err := store.InTx(ctx, s, func(tx store.Tx) error {
		if err := createCompany(ctx, tx, "outer"); err != nil {
			return err
		}
		err := store.InTx(ctx, tx, func(tx store.Tx) error {
			if err := createCompany(ctx, tx, "inner"); err != nil {
				return err
			}
			return errFail
		})
		if !errors.Is(err, errFail) {
			t.Errorf("inner InTx: err = %v, want errFail", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("outer InTx: %v", err)
	}
	if got := companyNames(t, ctx, s); len(got) != 1 || got[0] != "outer" {
		t.Errorf("companies = %q, want [outer]", got)
	}
}

// Every level shares one savepoint name, so this fails if ROLLBACK TO
// reached past the innermost one.
func TestInTx_TwoLevelsDeep_FailingInnermostUndoesOnlyItsOwnWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestStore(t)

	err := store.InTx(ctx, s, func(tx store.Tx) error {
		if err := createCompany(ctx, tx, "outer"); err != nil {
			return err
		}
		return store.InTx(ctx, tx, func(tx store.Tx) error {
			if err := createCompany(ctx, tx, "middle"); err != nil {
				return err
			}
			err := store.InTx(ctx, tx, func(tx store.Tx) error {
				if err := createCompany(ctx, tx, "inner"); err != nil {
					return err
				}
				return errFail
			})
			if !errors.Is(err, errFail) {
				t.Errorf("innermost InTx: err = %v, want errFail", err)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("outer InTx: %v", err)
	}
	got := companyNames(t, ctx, s)
	if diff := cmp.Diff([]string{"middle", "outer"}, got); diff != "" {
		t.Errorf("companies (-want +got):\n%s", diff)
	}
}

// The types can't stop a single-statement write being handed the outer Store
// inside InTx; this pins that it fails loudly rather than deadlocking.
func TestInTx_WriteOnOuterStore_FailsWithSQLiteBusy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := testutil.MigratedDBPath(t)
	cfg := store.DefaultConfig()
	cfg.BusyTimeout = 50 * time.Millisecond
	sqlDB, err := store.Open(path, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db := store.New(sqlDB)

	var outerErr error
	err = store.InTx(ctx, db, func(tx store.Tx) error {
		if err := createCompany(ctx, tx, "inside"); err != nil {
			return err
		}
		outerErr = createCompany(ctx, db, "outside")
		return nil
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if outerErr == nil || !strings.Contains(outerErr.Error(), "SQLITE_BUSY") {
		t.Errorf("write on the outer Store inside InTx: err = %v, want SQLITE_BUSY", outerErr)
	}
}

func TestInTx_WritesInATransactionAndASavepoint_StampTheirOrigin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := testutil.MigratedDBPath(t)
	s := testutil.OpenStore(t, path, "tui:1")

	err := store.InTx(ctx, s, func(tx store.Tx) error {
		if err := createCompany(ctx, tx, "in-tx"); err != nil {
			return err
		}
		return store.InTx(ctx, tx, func(tx store.Tx) error { return createCompany(ctx, tx, "in-savepoint") })
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if diff := cmp.Diff([]string{"tui:1", "tui:1"}, changeOrigins(t, s, "companies")); diff != "" {
		t.Errorf("company change origins (-want +got):\n%s", diff)
	}
}

func TestZeroStoreAndTx_ReturnAnErrorRatherThanPanic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, r := range []store.Runner{store.Store{}, store.Tx{}} {
		calls := map[string]func() error{
			"Read": func() error {
				_, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.Company, error) { return q.ListActiveCompanies(ctx) })
				return err
			},
			"Write": func() error { return createCompany(ctx, r, "a") },
			"InTx":  func() error { return store.InTx(ctx, r, func(store.Tx) error { return nil }) },
		}
		for name, call := range calls {
			t.Run(fmt.Sprintf("%T/%s", r, name), func(t *testing.T) {
				t.Parallel()
				if err := call(); err == nil {
					t.Errorf("%s on a zero %T: err = nil, want an error", name, r)
				}
			})
		}
	}
}

func TestRead_NoRows_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestStore(t)

	_, err := store.Read(ctx, s, func(q *sqlcdb.Queries) (sqlcdb.Company, error) {
		return q.GetCompany(ctx, 999)
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Read of a missing row: err = %v, want ErrNotFound", err)
	}
}

func changeOrigins(t *testing.T, s store.Store, table string) []string {
	t.Helper()
	events, err := service.NewChangeLog(s).After(context.Background(), 0)
	if err != nil {
		t.Fatalf("After: %v", err)
	}
	var origins []string
	for _, e := range events {
		if e.Table == table {
			origins = append(origins, e.Origin)
		}
	}
	return origins
}
