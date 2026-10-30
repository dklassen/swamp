package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

func testApplicationDetail() message.ApplicationDetail {
	return message.ApplicationDetail{
		Application: message.Application{ID: 1, PostingID: 5, Status: message.ApplicationStarted},
		Posting:     message.Posting{ID: 5, Title: "Engineer", ListingStatus: message.ListingOpen},
		CompanyName: "Acme",
	}
}

// applicationDetailOn is detail on d, with no services, its documents in
// docs.
func applicationDetailOn(docs *documents.Store, d message.ApplicationDetail) *applicationDetailModel {
	return newApplicationDetailModel(nil, nil, nil, nil, docs, d)
}

func applicationDetailView(m *applicationDetailModel) string { return m.View().Content }

// pressApplicationDetail sends key to m and returns what its command sends.
func pressApplicationDetail(t *testing.T, m *applicationDetailModel, key tea.KeyPressMsg) []tea.Msg {
	t.Helper()
	return runCmd(t, m.keyPressed(key))
}

func TestApplicationDetailModel_EscOrB_Pops(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('b')} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			m := applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail())

			if got := pressApplicationDetail(t, m, key); !popsIn(got, m.Instance()) {
				t.Errorf("%s sent %#v, want a pop of the detail", key, got)
			}
		})
	}
}

func TestApplicationDetailModel_P_PushesItsPostingsDetailWithTheApplication(t *testing.T) {
	t.Parallel()

	d := testApplicationDetail()
	m := applicationDetailOn(documents.NewStore(t.TempDir()), d)

	got := pressApplicationDetail(t, m, runeKey('p'))
	if len(got) != 1 {
		t.Fatalf("p sent %#v, want one Nav", got)
	}
	nav, ok := got[0].(message.Event[message.Nav[message.PostingDetail]])
	if !ok {
		t.Fatalf("p sent %#v, want a Nav to posting detail", got[0])
	}
	assertEventFrom(t, nav.Meta, m.src())
	want := message.Nav[message.PostingDetail]{Op: message.NavPush, Screen: message.ScreenPostingDetail, Value: message.PostingDetail{Posting: d.Posting, Application: d.Application}}
	if diff := cmp.Diff(want, nav.Payload); diff != "" {
		t.Errorf("Nav (-want +got):\n%s", diff)
	}
}

func TestApplicationDetailModel_D_PushesTheDeleteConfirmationOnWhatItShows(t *testing.T) {
	t.Parallel()

	d := testApplicationDetail()
	m := applicationDetailOn(documents.NewStore(t.TempDir()), d)

	got := pressApplicationDetail(t, m, runeKey('D'))
	if len(got) != 1 {
		t.Fatalf("D sent %#v, want one Nav", got)
	}
	nav, ok := got[0].(message.Event[message.Nav[message.ApplicationDetail]])
	if !ok {
		t.Fatalf("D sent %#v, want a Nav to the delete confirmation", got[0])
	}
	assertEventFrom(t, nav.Meta, m.src())
	want := message.Nav[message.ApplicationDetail]{Op: message.NavPush, Screen: message.ScreenApplicationDelete, Value: d}
	if diff := cmp.Diff(want, nav.Payload); diff != "" {
		t.Errorf("Nav (-want +got):\n%s", diff)
	}
}

// The screens App still opens are asked for with the application as it
// shows.
func TestApplicationDetailModel_AppsScreenKeys_AskForThemOnTheApplication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  rune
		want func(msg tea.Msg) bool
	}{
		{key: 'S', want: func(msg tea.Msg) bool {
			got, ok := msg.(enterApplicationSubmitMsg)
			return ok && got.application.ID == 1
		}},
		{key: 'f', want: func(msg tea.Msg) bool { got, ok := msg.(enterApplicationFormMsg); return ok && got.postingID == 5 }},
	}
	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			t.Parallel()
			m := applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail())

			if got := pressApplicationDetail(t, m, runeKey(tt.key)); len(got) != 1 || !tt.want(got[0]) {
				t.Errorf("%c sent %#v, want App asked for its screen on application 1", tt.key, got)
			}
		})
	}
}

func TestApplicationDetailModel_ShiftL_OnMissingCoverLetter_DoesNothing(t *testing.T) {
	t.Parallel()

	m := applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail())
	if got := pressApplicationDetail(t, m, runeKey('L')); got != nil {
		t.Fatalf("L sent %#v, want nothing: the cover letter doesn't exist yet", got)
	}
}

func TestApplicationDetailModel_View_ShowsOutcomeAndNotes(t *testing.T) {
	t.Parallel()

	d := testApplicationDetail()
	d.Reviews = []message.Review{
		{DocumentType: message.DocumentCoverLetter, Outcome: message.ReviewFlagged, Notes: "too generic"},
		{DocumentType: message.DocumentResume, Outcome: message.ReviewPassed},
	}
	m := applicationDetailOn(documents.NewStore(t.TempDir()), d)

	if got := applicationDetailView(m); !containsAll(got, "Acme", "[FLAGGED]", "too generic", "[PASSED]") {
		t.Fatalf("View() = %q, want company and both review outcomes with notes", got)
	}
}

func TestApplicationDetailModel_View_ShowsStatusLabelNotEnumValue(t *testing.T) {
	t.Parallel()

	d := testApplicationDetail()
	d.Application.Status = message.ApplicationOfferReceived
	got := applicationDetailView(applicationDetailOn(documents.NewStore(t.TempDir()), d))

	if want := applicationStatusLabel(application.OfferReceived); !strings.Contains(got, want) {
		t.Errorf("View() = %q, want the human-readable status label %q", got, want)
	}
	if raw := application.OfferReceived.String(); strings.Contains(got, raw) {
		t.Errorf("View() leaks the raw enum value %q into the UI", raw)
	}
}

// Every document type in documents' table gets its keys, its row and its
// help entry on application detail, without listing types here (RFC
// 0004): adding a type to the table is enough.
func TestApplicationDetailModel_EveryDocumentType(t *testing.T) {
	t.Parallel()

	// Keys application detail uses for itself; S is submit, f the form.
	reserved := map[rune]bool{'p': true, 'b': true, 'u': true, 's': true, 'f': true}
	docs := documents.NewStore(t.TempDir())
	for _, documentType := range document.Types() {
		p, err := docs.Path(1, documentType)
		if err != nil {
			t.Fatalf("Path: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(p, []byte("# "+documentType.String()), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	m := applicationDetailOn(docs, testApplicationDetail())
	view := applicationDetailView(m)

	for _, documentType := range document.Types() {
		key := documentType.Key()
		if reserved[key] {
			t.Errorf("%s's key %q is already used by application detail", documentType, key)
		}
		// Not run: it would open $EDITOR.
		if cmd := m.keyPressed(runeKey(key)); cmd == nil {
			t.Errorf("%q: no command to edit the %s", key, documentType.Label())
		}
		got := pressApplicationDetail(t, m, runeKey(unicode.ToUpper(key)))
		if len(got) != 1 {
			t.Fatalf("%q sent %#v, want one request for the review form", unicode.ToUpper(key), got)
		}
		review, ok := got[0].(enterDocumentReviewFormMsg)
		if !ok || review.applicationID != 1 || review.documentType != documentType || review.content != "# "+documentType.String() {
			t.Errorf("%q: sent %+v, want a review of the %s with its content", unicode.ToUpper(key), got[0], documentType.Label())
		}
		if title := documentTitle(documentType); !strings.Contains(view, title+":") {
			t.Errorf("View() has no %q row", title)
		}
		if !strings.Contains(view, string(key)+": edit "+documentType.Label()) || !strings.Contains(view, string(unicode.ToUpper(key))+": review "+documentType.Label()) {
			t.Errorf("View() help doesn't list the %s keys", documentType.Label())
		}
	}
}

// applicationDetailWith is detail registered on a router, opened on only
// the ID of an application in the store whose resume was reviewed.
func applicationDetailWith(t *testing.T) (*applicationDetailModel, *model.MsgRouter, testutil.TestApp) {
	t.Helper()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	p := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	app := mustCreateApplication(t, s, p.ID)
	docs := documents.NewStore(t.TempDir())
	if _, err := docs.Write(app.ID, document.Resume, "# Draft\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := createReview(context.Background(), s, app.ID, document.Resume, "# Draft\n", document.Passed, "reads well"); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	m := newApplicationDetailModel(s.Applications, s.Postings, s.Companies, s.Documents, docs, message.ApplicationDetail{Application: message.Application{ID: app.ID}})
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return m, r, s
}

func TestApplicationDetailModel_Init_ReadsTheApplicationItsPostingCompanyAndReviews(t *testing.T) {
	t.Parallel()

	m, r, _ := applicationDetailWith(t)
	settle(t, r, runCmd(t, m.Init()))

	if got := applicationDetailView(m); !containsAll(got, "Acme", applicationStatusLabel(application.Started), "[PASSED]", "reads well") {
		t.Errorf("View() after Init = %q, want the company, the status and the resume's review", got)
	}
	if m.detail.Posting.Title != "Engineer" {
		t.Errorf("posting after Init = %+v, want Engineer", m.detail.Posting)
	}
}

func TestApplicationDetailModel_ReadOfAnotherScreen_IsDropped(t *testing.T) {
	t.Parallel()

	m := applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail())
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	other := testApplicationDetail()
	other.CompanyName = "Globex"

	r.Dispatch(message.New(message.Source{Instance: m.Instance() + 1}, message.Subject{Type: message.TypeApplication, ID: 1}, message.OpNone, time.Now().UTC(),
		message.Read[message.ApplicationDetail]{Value: other}))

	if m.detail.CompanyName != "Acme" {
		t.Errorf("company after another screen's read = %q, want Acme", m.detail.CompanyName)
	}
}

// What makes detail read again, and what doesn't.
func TestApplicationDetailModel_ReadsAgain(t *testing.T) {
	t.Parallel()

	edited := func(from message.ScreenInstance, applicationID int64) tea.Msg {
		return message.New(message.Source{Instance: from}, message.Subject{Type: message.TypeApplication, ID: applicationID}, message.OpNone, time.Now().UTC(),
			message.Ran[message.EditDocument]{Value: message.EditDocument{DocumentType: message.DocumentResume}})
	}
	tests := []struct {
		name string
		// msgs are sent, in order, once detail has read while on top.
		msgs func(m *applicationDetailModel) []tea.Msg
		want bool
	}{
		{name: "on a load request", want: true, msgs: func(*applicationDetailModel) []tea.Msg {
			return []tea.Msg{message.New(message.Source{Instance: 9}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[message.ApplicationDetail]{})}
		}},
		{name: "on its own edit", want: true, msgs: func(m *applicationDetailModel) []tea.Msg {
			return []tea.Msg{edited(m.Instance(), m.detail.Application.ID)}
		}},
		{name: "not on another screen's edit", msgs: func(m *applicationDetailModel) []tea.Msg {
			return []tea.Msg{edited(m.Instance()+1, m.detail.Application.ID)}
		}},
		{name: "on coming back on top", want: true, msgs: func(m *applicationDetailModel) []tea.Msg {
			return []tea.Msg{focusOn(0), focusOn(m.Instance())}
		}},
		{name: "not on staying on top", msgs: func(m *applicationDetailModel) []tea.Msg {
			return []tea.Msg{focusOn(m.Instance())}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, r, _ := applicationDetailWith(t)
			settle(t, r, runCmd(t, m.Init()))
			settle(t, r, runCmd(t, r.Dispatch(focusOn(m.Instance()))))

			var got []tea.Msg
			for _, msg := range tt.msgs(m) {
				got = append(got, runCmd(t, r.Dispatch(msg))...)
			}
			reads := 0
			for _, msg := range got {
				e, ok := msg.(message.Event[message.Read[message.ApplicationDetail]])
				if !ok {
					t.Fatalf("sent %#v, want only its read", got)
				}
				assertEventFrom(t, e.Meta, m.src())
				reads++
			}
			if want := map[bool]int{true: 1}[tt.want]; reads != want {
				t.Errorf("sent %d reads, want %d", reads, want)
			}
		})
	}
}

// A write to the application shows whoever made it, including one made
// while another screen covers the detail.
func TestApplicationDetailModel_ApplicationChanged_AboutItsApplication_PatchesIt(t *testing.T) {
	t.Parallel()

	start := testApplicationDetail().Application
	interviewing := start
	interviewing.Status = message.ApplicationInterviewing
	tests := []struct {
		name    string
		changed tea.Msg
		want    message.Application
	}{
		{name: "updated", changed: message.Updated(message.Source{Instance: 7}, 1, time.Now().UTC(), start, interviewing), want: interviewing},
		{name: "deleted", changed: message.Deleted(message.Source{Instance: 7}, 1, time.Now().UTC(), start), want: start},
		{name: "another application's", changed: message.Updated(message.Source{Instance: 7}, 2, time.Now().UTC(),
			message.Application{ID: 2, PostingID: 6}, message.Application{ID: 2, PostingID: 6, Status: message.ApplicationRejected}), want: start},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail())
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(tt.changed)
			if m.detail.Application != tt.want {
				t.Errorf("application = %+v, want %+v", m.detail.Application, tt.want)
			}
		})
	}
}

// Its read finding no application means it was deleted elsewhere.
func TestApplicationDetailModel_ReadFindsItGone_PopsAndSaysSo(t *testing.T) {
	t.Parallel()

	m, r, s := applicationDetailWith(t)
	if _, err := s.Applications.Delete(context.Background(), m.detail.Application.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got := runCmd(t, r.Dispatch(runCmd(t, m.Init())[0]))
	if !popsIn(got, m.Instance()) {
		t.Errorf("sent %#v, want a pop of the detail", got)
	}
	if !notifiesIn(got, "The application you were viewing was deleted elsewhere") {
		t.Errorf("sent %#v, want a status saying it was deleted elsewhere", got)
	}
}

func TestApplicationDetailModel_OtherErrors_LeaveItUp(t *testing.T) {
	t.Parallel()

	m := applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail())
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}

	failed := message.New(m.src(), message.Subject{Type: message.TypeApplication, ID: 1}, message.OpNone, time.Now().UTC(), model.ErrorOf(errors.New("disk on fire")))
	if got := runCmd(t, r.Dispatch(failed)); got != nil {
		t.Errorf("sent %#v, want nothing: the banner shows the error", got)
	}
	gone := message.New(message.Source{Instance: m.Instance() + 1}, message.Subject{Type: message.TypeApplication, ID: 1}, message.OpNone, time.Now().UTC(), model.ErrorOf(store.ErrNotFound))
	if got := runCmd(t, r.Dispatch(gone)); got != nil {
		t.Errorf("another screen's not found sent %#v, want nothing", got)
	}
}

// notifiesIn is whether any of msgs puts status on the status line.
func notifiesIn(msgs []tea.Msg, status string) bool {
	for _, msg := range msgs {
		if n, ok := msg.(message.Event[message.Notify]); ok && n.Payload.Status == status {
			return true
		}
	}
	return false
}

func TestApplicationDetailModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail()))
}

// Deleted from the confirmation over it, the detail has nothing left to
// show: it leaves once it's back on top, without a read that would say the
// application was deleted elsewhere.
func TestApplicationDetailModel_DeletedWhileCovered_PopsOnComingBackWithoutReading(t *testing.T) {
	t.Parallel()

	m, r, s := applicationDetailWith(t)
	confirmation := message.Source{Instance: message.NewScreenInstance()}
	r.Dispatch(focusOn(confirmation.Instance))
	deleted, err := s.Applications.Delete(context.Background(), m.detail.Application.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := runCmd(t, r.Dispatch(applicationWritten(confirmation, m.detail.Application.ID, message.OpDeleted, deleted, nil))); got != nil {
		t.Errorf("the delete sent %#v while covered, want nothing until it's back on top", got)
	}

	got := runCmd(t, r.Dispatch(focusOn(m.Instance())))

	if len(got) != 1 || !pops(got[0], m.Instance()) {
		t.Errorf("coming back on top sent %#v, want only a pop of the detail", got)
	}
}

func TestApplicationDetailModel_DeletedWhileOnTop_PopsAndSaysSo(t *testing.T) {
	t.Parallel()

	m := applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail())
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Dispatch(focusOn(m.Instance()))

	got := runCmd(t, r.Dispatch(message.Deleted(message.Source{Instance: message.NewScreenInstance()}, 1, time.Now().UTC(), testApplicationDetail().Application)))

	if !popsIn(got, m.Instance()) {
		t.Errorf("sent %#v, want a pop of the detail", got)
	}
	if !notifiesIn(got, "The application you were viewing was deleted elsewhere") {
		t.Errorf("sent %#v, want a status saying it was deleted elsewhere", got)
	}
}

// Once its application is deleted, a load the change log asks for or a
// read already on its way doesn't say it was deleted elsewhere over the
// confirmation's word.
func TestApplicationDetailModel_Deleted_IgnoresLoadsAndItsLateRead(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		msg  func(m *applicationDetailModel) tea.Msg
	}{
		{name: "a load request", msg: func(*applicationDetailModel) tea.Msg {
			return message.New(message.Source{Instance: 9}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[message.ApplicationDetail]{})
		}},
		{name: "its read, finding nothing", msg: func(m *applicationDetailModel) tea.Msg {
			return message.New(m.src(), message.Subject{Type: message.TypeApplication, ID: 1}, message.OpNone, time.Now().UTC(), model.ErrorOf(store.ErrNotFound))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := applicationDetailOn(documents.NewStore(t.TempDir()), testApplicationDetail())
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}
			r.Dispatch(message.Deleted(message.Source{Instance: message.NewScreenInstance()}, 1, time.Now().UTC(), testApplicationDetail().Application))

			if got := runCmd(t, r.Dispatch(tt.msg(m))); got != nil {
				t.Errorf("sent %#v, want nothing", got)
			}
		})
	}
}
