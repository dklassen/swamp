package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// testExportApplication is the application every export-screen test
// drafts documents for.
var testExportApplication = message.ApplicationDetail{
	Application: message.Application{ID: 42, PostingID: 7, Status: message.ApplicationStarted},
	Posting:     message.Posting{ID: 7, Title: "Delivery Platform", ListingStatus: message.ListingOpen},
	CompanyName: "Northwind",
}

// writeApplicationDocuments creates a documents base dir containing the
// named markdown documents for applicationID, and returns a Store over
// it. Document types not named are left absent on disk.
func writeApplicationDocuments(t *testing.T, applicationID int64, present ...document.Type) *documents.Store {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, strconv.FormatInt(applicationID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir documents dir: %v", err)
	}
	for _, documentType := range present {
		path := filepath.Join(dir, documentType.String()+".md")
		if err := os.WriteFile(path, []byte("# Heading\n\nBody text.\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return documents.NewStore(base)
}

func exportScreenOn(t *testing.T, docs *documents.Store, dir string) *applicationExportModel {
	t.Helper()
	return newApplicationExportModel(testutil.NewTestApp(t).Documents, docs, testExportApplication, dir, message.ContentSize{Width: 80})
}

// exportedIn is the export report among msgs.
func exportedIn(t *testing.T, msgs []tea.Msg) message.Event[message.Ran[message.ExportReport]] {
	t.Helper()
	for _, msg := range msgs {
		if e, ok := msg.(message.Event[message.Ran[message.ExportReport]]); ok {
			return e
		}
	}
	t.Fatalf("no export report in %+v", msgs)
	return message.Event[message.Ran[message.ExportReport]]{}
}

func TestApplicationExportModel_PrefillsTheDestination(t *testing.T) {
	t.Parallel()

	m := exportScreenOn(t, writeApplicationDocuments(t, 42, document.CoverLetter), "/home/dana/Desktop")

	if got := m.textinput.Value(); got != "/home/dana/Desktop" {
		t.Errorf("destination prefill = %q, want %q", got, "/home/dana/Desktop")
	}
	if view := m.View().Content; !strings.Contains(view, "Northwind") || !strings.Contains(view, "Delivery Platform") {
		t.Errorf("View() = %q, want it to name the application being exported", view)
	}
}

func TestApplicationExportModel_EscPopsAndClearsTheBanner(t *testing.T) {
	t.Parallel()

	m := exportScreenOn(t, writeApplicationDocuments(t, 42, document.CoverLetter), "/tmp")

	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}))

	if !popsIn(got, m.Instance()) {
		t.Errorf("esc sent %+v, want a pop", got)
	}
	if status, ok := statusIn(got); !ok || status != "" {
		t.Errorf("esc status = %q (sent %v), want the banner cleared", status, ok)
	}
}

func TestApplicationExportModel_EnterExportsBothDocumentsWithDescriptiveNames(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	m := exportScreenOn(t, writeApplicationDocuments(t, 42, document.CoverLetter, document.Resume), dest)

	got := exportedIn(t, runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})))

	if got.Meta.Source.Instance != m.Instance() {
		t.Errorf("report from %v, want the screen %v", got.Meta.Source.Instance, m.Instance())
	}
	if want := (message.Subject{Type: message.TypeApplication, ID: 42}); got.Meta.Subject != want {
		t.Errorf("report about %+v, want %+v", got.Meta.Subject, want)
	}
	want := message.ExportReport{Dir: dest, Paths: []string{
		filepath.Join(dest, "northwind-delivery-platform-cover_letter.pdf"),
		filepath.Join(dest, "northwind-delivery-platform-resume.pdf"),
	}}
	if diff := cmp.Diff(want, got.Payload.Value); diff != "" {
		t.Errorf("report mismatch (-want +got):\n%s", diff)
	}
	for _, path := range want.Paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read exported pdf: %v", err)
		}
		if len(content) < 4 || string(content[:4]) != "%PDF" {
			t.Errorf("%s does not start with %%PDF", path)
		}
	}
}

func TestApplicationExportModel_SkipsUndraftedDocumentsWithoutFailing(t *testing.T) {
	t.Parallel()

	// Resume drafted, cover letter never written.
	dest := t.TempDir()
	m := exportScreenOn(t, writeApplicationDocuments(t, 42, document.Resume), dest)

	got := exportedIn(t, runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})))

	want := message.ExportReport{
		Dir:     dest,
		Paths:   []string{filepath.Join(dest, "northwind-delivery-platform-resume.pdf")},
		Skipped: []message.DocumentType{documentTypeSnapshots[document.CoverLetter]},
	}
	if diff := cmp.Diff(want, got.Payload.Value); diff != "" {
		t.Errorf("report mismatch (-want +got):\n%s", diff)
	}
}

func TestApplicationExportModel_ExpandsTildeInDestination(t *testing.T) {
	// Not parallel: t.Setenv can't be used with t.Parallel.
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := exportScreenOn(t, writeApplicationDocuments(t, 42, document.Resume), "~/Desktop")

	got := exportedIn(t, runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})))

	wantDir := filepath.Join(home, "Desktop")
	if got.Payload.Value.Dir != wantDir {
		t.Errorf("dir = %q, want %q (tilde expanded)", got.Payload.Value.Dir, wantDir)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "northwind-delivery-platform-resume.pdf")); err != nil {
		t.Errorf("stat exported pdf: %v (want it written under the expanded home dir)", err)
	}
}

// Each exported document is recorded with the content it was exported
// from, which is what tells "passed, not exported" apart from
// "exported, ready to submit".
func TestApplicationExportModel_RecordsEachExport(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	dest := t.TempDir()
	m := newApplicationExportModel(s.Documents, writeApplicationDocuments(t, 42, document.CoverLetter), testExportApplication, dest, message.ContentSize{Width: 80})

	exportedIn(t, runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})))

	exports, err := s.Documents.LatestExports(context.Background(), 42)
	if err != nil {
		t.Fatalf("LatestExports: %v", err)
	}
	coverLetter, ok := exports[document.CoverLetter]
	if !ok {
		t.Fatal("no cover letter export recorded")
	}
	if want := filepath.Join(dest, "northwind-delivery-platform-cover_letter.pdf"); coverLetter.Path != want {
		t.Errorf("export path = %q, want %q", coverLetter.Path, want)
	}
	if !coverLetter.IsCurrent("# Heading\n\nBody text.\n") {
		t.Error("export isn't current for the content that was exported")
	}
	if _, ok := exports[document.Resume]; ok {
		t.Error("a resume export was recorded, but there was no resume to export")
	}
}

func TestApplicationExportModel_AFailedExportIsAnError(t *testing.T) {
	t.Parallel()

	// A path under a regular file can't be created as a directory.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := exportScreenOn(t, writeApplicationDocuments(t, 42, document.Resume), filepath.Join(blocker, "sub"))

	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}))

	if len(got) != 1 {
		t.Fatalf("enter sent %+v, want one error", got)
	}
	e, ok := got[0].(message.Event[message.Error])
	if !ok {
		t.Fatalf("enter sent %T, want an error", got[0])
	}
	if e.Meta.Source.Instance != m.Instance() || e.Meta.Subject != (message.Subject{Type: message.TypeApplication, ID: 42}) {
		t.Errorf("error meta = %+v, want from the screen, about its application", e.Meta)
	}
}

// Its own export closes it, saying where the files went, since dragging
// them from there is what the screen is for.
func TestApplicationExportModel_ItsExportPopsWithWhereTheFilesWent(t *testing.T) {
	t.Parallel()

	m := exportScreenOn(t, writeApplicationDocuments(t, 42, document.Resume), "/tmp")
	report := message.ExportReport{Dir: "/out", Paths: []string{"/out/northwind-delivery-platform-resume.pdf"}}

	got := runCmd(t, m.exported(message.New(m.src(), message.Subject{Type: message.TypeApplication, ID: 42}, message.OpNone, time.Now().UTC(), message.Ran[message.ExportReport]{Value: report})))

	if !popsIn(got, m.Instance()) {
		t.Errorf("its export sent %+v, want a pop", got)
	}
	if status, _ := statusIn(got); status != exportStatusLine(report) {
		t.Errorf("status = %q, want %q", status, exportStatusLine(report))
	}
}

// The route that hears a failure doesn't exist: the screen stays for the
// user to fix the path they can still see, and App's banner shows why.
func TestApplicationExportModel_StaysOnAFailedExport(t *testing.T) {
	t.Parallel()

	m := exportScreenOn(t, writeApplicationDocuments(t, 42, document.Resume), "/tmp")
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cmd := r.Dispatch(message.New(m.src(), message.Subject{Type: message.TypeApplication, ID: 42}, message.OpNone, time.Now().UTC(), message.Error{Code: message.ErrOther, Text: "create /x: not a directory"}))

	if got := runCmd(t, cmd); popsIn(got, m.Instance()) {
		t.Errorf("a failed export sent %+v, want the screen kept", got)
	}
}
