package tui

import (
	"errors"
	"time"

	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// deleteListOf is a company list of Acme, with three open postings, and
// Globex, with the cursor on Acme.
func deleteListOf(t *testing.T) (*companyListModel, company.Company) {
	t.Helper()
	acme := company.Company{ID: 1, Name: "Acme", Source: "ashby"}
	globex := company.Company{ID: 2, Name: "Globex", Source: "lever"}
	return companyListOf(t, []company.Company{acme, globex}, map[int64]int{acme.ID: 3}, 100, 30), acme
}

// d opens the confirmation over the list, which stays in view behind it.
func TestCompanyListModel_D_OpensTheDeleteConfirmation(t *testing.T) {
	t.Parallel()
	m, _ := deleteListOf(t)

	if got := press(t, m, runeKey('d')); got != nil {
		t.Errorf("d sent %#v, want nothing: the list opens the confirmation itself", got)
	}

	view := ansi.Strip(companyListView(m))
	if !strings.Contains(view, "Delete Acme?") || !strings.Contains(view, "open postings (3)") || !strings.Contains(view, "y: delete") {
		t.Errorf("no confirmation naming Acme and its open postings:\n%s", view)
	}
	if !strings.Contains(view, "Globex") {
		t.Errorf("the list isn't in view behind the confirmation:\n%s", view)
	}
}

// Keeping is the default, so enter can't delete by habit. The key closes
// the confirmation and goes no further.
func TestCompanyListModel_DeleteConfirmation_KeepKeysCloseIt(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{runeKey('n'), {Code: tea.KeyEsc}, {Code: tea.KeyEnter}} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			m, _ := deleteListOf(t)
			press(t, m, runeKey('d'))

			if got := press(t, m, key); got != nil {
				t.Errorf("%s sent %#v, want nothing", key, got)
			}

			if view := ansi.Strip(companyListView(m)); strings.Contains(view, "Delete Acme?") {
				t.Errorf("the confirmation is still drawn after %s:\n%s", key, view)
			}
		})
	}
}

// The confirmation takes every key while it's open; the first one after
// it closes is the list's.
func TestCompanyListModel_DeleteConfirmation_HoldsTheListsKeysUntilItCloses(t *testing.T) {
	t.Parallel()
	m, _ := deleteListOf(t)
	press(t, m, runeKey('d'))

	press(t, m, runeKey('j'))
	if m.cursor != 0 {
		t.Errorf("cursor after j with the confirmation open = %d, want 0", m.cursor)
	}

	press(t, m, runeKey('n'))
	press(t, m, runeKey('j'))
	if m.cursor != 1 {
		t.Errorf("cursor after closing and j = %d, want 1", m.cursor)
	}
}

// The confirmation closes once its company is gone, whoever deleted it,
// clearing an earlier failure from the banner, and when its own delete
// fails. A late failure from an earlier confirmation leaves this one up.
func TestCompanyListModel_DeleteConfirmation_ClosesWhenItsCompanyIsDeletedOrItsDeleteFails(t *testing.T) {
	t.Parallel()

	other := message.Source{Instance: message.NewScreenInstance()}
	failed := func(src message.Source, id int64) tea.Msg {
		return message.New(src, message.Subject{Type: message.TypeCompany, ID: id}, message.OpDeleted, time.Now().UTC(), model.ErrorOf(errors.New("disk full")))
	}
	for _, tt := range []struct {
		name      string
		event     func(confirm *companyDeleteModel) tea.Msg
		wantClose bool
		// wantCleared is whether the banner is cleared.
		wantCleared bool
	}{
		{name: "its company, deleted by it", event: func(c *companyDeleteModel) tea.Msg {
			return message.Deleted(message.Source{Instance: c.instance}, c.company.ID, time.Now().UTC(), c.company)
		}, wantClose: true, wantCleared: true},
		{name: "its company, deleted elsewhere", event: func(c *companyDeleteModel) tea.Msg {
			return message.Deleted(other, c.company.ID, time.Now().UTC(), c.company)
		}, wantClose: true, wantCleared: true},
		{name: "its delete, failed", event: func(c *companyDeleteModel) tea.Msg {
			return failed(message.Source{Instance: c.instance}, c.company.ID)
		}, wantClose: true},
		{name: "another company, deleted", event: func(*companyDeleteModel) tea.Msg {
			return message.Deleted(other, 2, time.Now().UTC(), message.Company{ID: 2, Name: "Globex"})
		}},
		{name: "its company, renamed", event: func(c *companyDeleteModel) tea.Msg {
			after := c.company
			after.Name = "Acme Corp"
			return message.Updated(other, c.company.ID, time.Now().UTC(), c.company, after)
		}},
		{name: "an earlier confirmation's delete, failed", event: func(c *companyDeleteModel) tea.Msg {
			return failed(other, c.company.ID)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := deleteListOf(t)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}
			press(t, m, runeKey('d'))

			got := runCmd(t, r.Dispatch(tt.event(m.confirm)))

			if closed := m.confirm == nil; closed != tt.wantClose {
				t.Errorf("closed = %v, want %v", closed, tt.wantClose)
			}
			if cleared := notifiesIn(got, ""); cleared != tt.wantCleared {
				t.Errorf("banner cleared = %v, want %v (sent %#v)", cleared, tt.wantCleared, got)
			}
		})
	}
}

// y deletes the company once, from the confirmation, so its result is
// told from an earlier one's; a second y while it's in flight sends
// nothing.
func TestCompanyListModel_DeleteConfirmation_Y_DeletesOnce(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	m := newCompanyListModel(s.Companies, nil)
	m.companiesRead(companiesReadOf([]company.Company{acme}, nil))
	press(t, m, runeKey('d'))
	from := message.Source{Instance: m.confirm.instance}

	msg := press(t, m, runeKey('y'))

	got, ok := msg.(message.Event[message.Changed[message.Company]])
	if !ok {
		t.Fatalf("y sent %T, want message.Event[message.Changed[message.Company]]", msg)
	}
	want := message.Deleted(from, acme.ID, got.Meta.At, companySnapshot(acme))
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(message.Meta{}, "ID")); diff != "" {
		t.Errorf("y sent (-want +got):\n%s", diff)
	}
	if again := press(t, m, runeKey('y')); again != nil {
		t.Errorf("a second y sent %#v, want nothing while the first is in flight", again)
	}
}

// Nothing under the box takes a paste.
func TestCompanyListModel_DeleteConfirmation_PasteChangesNothing(t *testing.T) {
	t.Parallel()
	m, _ := deleteListOf(t)
	press(t, m, runeKey('d'))
	before := companyListView(m)

	m.paste(tea.PasteMsg{Content: "glob"})

	if after := companyListView(m); after != before {
		t.Errorf("view changed on a paste with the confirmation open:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
