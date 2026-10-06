package migrations

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestChangeLog_EveryTableIsClassified: a new table must be listed as
// logged or not, so adding one is a decision rather than a silent gap in
// what other processes can see.
func TestChangeLog_EveryTableIsClassified(t *testing.T) {
	t.Parallel()

	sqlDB := migrateTo(t, Latest())
	rows, err := sqlDB.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		_, logged := Logged[name]
		_, notLogged := NotLogged[name]
		switch {
		case logged && notLogged:
			t.Errorf("%s is listed both as logged and as not logged", name)
		case !logged && !notLogged:
			t.Errorf("%s isn't in Logged or NotLogged (changelog.go): decide whether its changes go in the change log", name)
		}
	}
}

// TestChangeLog_EveryListedTableExists: a renamed or dropped table would
// otherwise leave a stale entry that no longer describes anything.
func TestChangeLog_EveryListedTableExists(t *testing.T) {
	t.Parallel()

	sqlDB := migrateTo(t, Latest())
	listed := make([]string, 0, len(Logged)+len(NotLogged))
	for name := range Logged {
		listed = append(listed, name)
	}
	for name := range NotLogged {
		listed = append(listed, name)
	}
	for _, name := range listed {
		var n int
		if err := sqlDB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("look up %s: %v", name, err)
		}
		if n == 0 {
			t.Errorf("%s is listed in changelog.go but isn't a table", name)
		}
	}
}

// TestChangeLog_EveryColumnOfALoggedTableIsClassified: a column added to a
// logged table must be logged or ignored on purpose; one left out of both
// would change without other processes hearing of it.
func TestChangeLog_EveryColumnOfALoggedTableIsClassified(t *testing.T) {
	t.Parallel()

	sqlDB := migrateTo(t, Latest())
	for table, logged := range Logged {
		listed := map[string]int{logged.Key: 1}
		for _, c := range append(append([]string{}, logged.Columns...), logged.Ignored...) {
			listed[c]++
		}
		rows, err := sqlDB.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		actual := map[string]bool{}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatalf("scan: %v", err)
			}
			actual[c] = true
		}
		_ = rows.Close()

		for c := range actual {
			switch listed[c] {
			case 0:
				t.Errorf("%s.%s is neither logged nor ignored (changelog.go)", table, c)
			case 1:
			default:
				t.Errorf("%s.%s is listed more than once (changelog.go)", table, c)
			}
		}
		for c := range listed {
			if !actual[c] {
				t.Errorf("%s.%s is listed in changelog.go but isn't a column", table, c)
			}
		}
	}
}

// TestChangeLog_TriggersMatchTheList: rebuilding a table drops its
// triggers, and a trigger written for an older column list goes on
// logging the old columns; either way changes go unseen. An update
// trigger whose WHEN misses a column never logs a change to it.
func TestChangeLog_TriggersMatchTheList(t *testing.T) {
	t.Parallel()

	sqlDB := migrateTo(t, Latest())
	ref := regexp.MustCompile(`\b(?:NEW|OLD)\.(\w+)`)
	refs := func(sql string) []string {
		seen := map[string]bool{}
		for _, m := range ref.FindAllStringSubmatch(sql, -1) {
			seen[m[1]] = true
		}
		return slices.Sorted(maps.Keys(seen))
	}
	for table, logged := range Logged {
		want := slices.Sorted(slices.Values(append([]string{logged.Key}, logged.Columns...)))
		for _, op := range []string{"insert", "update", "delete"} {
			name := table + "_change_" + op
			var sql string
			err := sqlDB.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ? AND tbl_name = ?`, name, table).Scan(&sql)
			if !slices.Contains(logged.Ops, op) {
				if err == nil {
					t.Errorf("trigger %s exists, but changelog.go doesn't log %s on %s", name, op, table)
				}
				continue
			}
			if err != nil {
				t.Errorf("trigger %s: %v (recreate it in the migration that rebuilt %s)", name, err, table)
				continue
			}
			if got := refs(sql); !slices.Equal(got, want) {
				t.Errorf("trigger %s refers to %v, want %v (changelog.go)", name, got, want)
			}
			if op == "update" {
				upper := strings.ToUpper(sql)
				when := sql[strings.Index(upper, " WHEN ")+len(" WHEN ") : strings.Index(upper, "BEGIN")]
				if got, wantWhen := refs(when), slices.Sorted(slices.Values(slices.Clone(logged.Columns))); !slices.Equal(got, wantWhen) {
					t.Errorf("trigger %s's WHEN checks %v, want every logged column %v", name, got, wantWhen)
				}
			}
		}
	}
}
