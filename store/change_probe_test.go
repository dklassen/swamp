package store

import (
	"context"
	"testing"
)

// mustNewChangeProbe is s.NewChangeProbe, closed when the test ends.
func mustNewChangeProbe(t *testing.T, s *Store) *ChangeProbe {
	t.Helper()
	p, err := s.NewChangeProbe(context.Background())
	if err != nil {
		t.Fatalf("NewChangeProbe: %v", err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return p
}

// mustChanged is p.Changed, failing the test on an error.
func mustChanged(t *testing.T, p *ChangeProbe) bool {
	t.Helper()
	changed, err := p.Changed(context.Background())
	if err != nil {
		t.Fatalf("Changed: %v", err)
	}
	return changed
}

// TestChangeProbe_SeesAnotherProcessCommit: the TUI's probe must notice a
// commit from mcp-serve or swamp fetch, which use their own connections
// to the same file (RFC 0007, step 7).
func TestChangeProbe_SeesAnotherProcessCommit(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/test.db"
	tui := newTestStoreAt(t, path)
	other := newTestStoreAt(t, path)
	p := mustNewChangeProbe(t, tui)

	if mustChanged(t, p) {
		t.Fatal("Changed before any commit = true, want false")
	}
	mustCreateCompany(t, other, "Acme", "ashby", "acme")
	if !mustChanged(t, p) {
		t.Fatal("Changed after another handle committed = false, want true")
	}
	if mustChanged(t, p) {
		t.Error("Changed again with no new commit = true, want false (it reports each change once)")
	}
}

// TestChangeProbe_SeesItsOwnPoolCommit: the TUI's own writes go through
// the same Store's pool, on other connections than the probe's, so they
// count too. That makes the TUI reload after its own changes, which it
// does anyway; what matters is that no commit is missed.
func TestChangeProbe_SeesItsOwnPoolCommit(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	p := mustNewChangeProbe(t, s)

	mustCreateCompany(t, s, "Acme", "ashby", "acme")

	if !mustChanged(t, p) {
		t.Error("Changed after a commit through the same Store = false, want true")
	}
}

// TestChangeProbe_IgnoresReads: an agent listing postings mustn't make the
// TUI reload.
func TestChangeProbe_IgnoresReads(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/test.db"
	tui := newTestStoreAt(t, path)
	other := newTestStoreAt(t, path)
	p := mustNewChangeProbe(t, tui)

	if _, err := other.ListActiveCompanies(context.Background()); err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}

	if mustChanged(t, p) {
		t.Error("Changed after only a read = true, want false")
	}
}
