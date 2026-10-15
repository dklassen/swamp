package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/store/sqlcdb"
)

var errFail = errors.New("fail")

func createTag(ctx context.Context, r Runner, name string) error {
	return Write(ctx, r, func(q *sqlcdb.Queries) error {
		_, err := q.CreateTag(ctx, name)
		return err
	})
}

func tagNames(t *testing.T, ctx context.Context, r Runner) []string {
	t.Helper()
	tags, err := Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.Tag, error) {
		return q.ListTags(ctx)
	})
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	names := make([]string, len(tags))
	for i, tag := range tags {
		names[i] = tag.Name
	}
	return names
}

func TestInTx_FnFails_RollsBackItsWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	err := InTx(ctx, s.DB(), func(tx Tx) error {
		if err := createTag(ctx, tx, "a"); err != nil {
			return err
		}
		return errFail
	})
	if !errors.Is(err, errFail) {
		t.Fatalf("InTx: err = %v, want errFail", err)
	}
	if got := tagNames(t, ctx, s.DB()); len(got) != 0 {
		t.Errorf("tags after a failed InTx = %q, want none", got)
	}
}

// The outer fn carrying on after the inner failure is what a joined
// transaction would get wrong: the inner write would commit with it.
func TestInTx_OnTx_FailingFnUndoesOnlyItsOwnWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	err := InTx(ctx, s.DB(), func(tx Tx) error {
		if err := createTag(ctx, tx, "outer"); err != nil {
			return err
		}
		err := InTx(ctx, tx, func(tx Tx) error {
			if err := createTag(ctx, tx, "inner"); err != nil {
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
	if got := tagNames(t, ctx, s.DB()); len(got) != 1 || got[0] != "outer" {
		t.Errorf("tags = %q, want [outer]", got)
	}
}

// Every level shares one savepoint name, so this fails if ROLLBACK TO
// reached past the innermost one.
func TestInTx_TwoLevelsDeep_FailingInnermostUndoesOnlyItsOwnWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	err := InTx(ctx, s.DB(), func(tx Tx) error {
		if err := createTag(ctx, tx, "outer"); err != nil {
			return err
		}
		return InTx(ctx, tx, func(tx Tx) error {
			if err := createTag(ctx, tx, "middle"); err != nil {
				return err
			}
			err := InTx(ctx, tx, func(tx Tx) error {
				if err := createTag(ctx, tx, "inner"); err != nil {
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
	got := tagNames(t, ctx, s.DB())
	if diff := cmp.Diff([]string{"middle", "outer"}, got); diff != "" {
		t.Errorf("tags (-want +got):\n%s", diff)
	}
}

// The types can't stop a single-statement write being handed the outer DB
// inside InTx; this pins that it fails loudly rather than deadlocking.
func TestInTx_WriteOnOuterDB_FailsWithSQLiteBusy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := t.TempDir() + "/test.db"
	newTestStoreAt(t, path) // migrate
	cfg := DefaultConfig()
	cfg.BusyTimeout = 50 * time.Millisecond
	sqlDB, err := Open(path, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db := New(sqlDB).DB()

	var outerErr error
	err = InTx(ctx, db, func(tx Tx) error {
		if err := createTag(ctx, tx, "inside"); err != nil {
			return err
		}
		outerErr = createTag(ctx, db, "outside")
		return nil
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if outerErr == nil || !strings.Contains(outerErr.Error(), "SQLITE_BUSY") {
		t.Errorf("write on the outer DB inside InTx: err = %v, want SQLITE_BUSY", outerErr)
	}
}

func TestInTx_WritesInATransactionAndASavepoint_StampTheirOrigin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := t.TempDir() + "/test.db"
	newTestStoreAt(t, path) // migrate
	s := openWithOrigin(t, path, "tui:1")
	createCompany := func(r Runner, name string) error {
		return Write(ctx, r, func(q *sqlcdb.Queries) error {
			_, err := q.CreateCompany(ctx, sqlcdb.CreateCompanyParams{Name: name, Source: "ashby", SourceRef: name})
			return err
		})
	}

	err := InTx(ctx, s.DB(), func(tx Tx) error {
		if err := createCompany(tx, "in-tx"); err != nil {
			return err
		}
		return InTx(ctx, tx, func(tx Tx) error { return createCompany(tx, "in-savepoint") })
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if diff := cmp.Diff([]string{"tui:1", "tui:1"}, changeOrigins(t, s, "companies")); diff != "" {
		t.Errorf("company change origins (-want +got):\n%s", diff)
	}
}

func TestZeroDBAndTx_ReturnAnErrorRatherThanPanic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, r := range []Runner{DB{}, Tx{}} {
		calls := map[string]func() error{
			"Read": func() error {
				_, err := Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.Tag, error) { return q.ListTags(ctx) })
				return err
			},
			"Write": func() error { return createTag(ctx, r, "a") },
			"InTx":  func() error { return InTx(ctx, r, func(Tx) error { return nil }) },
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
	s := newTestStore(t)

	_, err := Read(ctx, s.DB(), func(q *sqlcdb.Queries) (sqlcdb.Tag, error) {
		return q.GetTagByName(ctx, "missing")
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Read of a missing row: err = %v, want ErrNotFound", err)
	}
}
