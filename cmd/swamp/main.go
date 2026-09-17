// Command swamp is the entrypoint: it launches the TUI by default, runs a
// one-off refresh with the `fetch` subcommand, drives the agent hand-off
// mechanism with the `stage` subcommand, converts an application's
// drafted documents to PDF with the `export` subcommand, or bulk-creates
// companies from a YAML seed file with the `import` subcommand. Not unit
// tested per this project's testing decisions -- verified manually.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/ashby"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/export"
	"github.com/dklassen/swamp/greenhouse"
	"github.com/dklassen/swamp/lever"
	"github.com/dklassen/swamp/seed"
	"github.com/dklassen/swamp/stage"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui"
)

func main() {
	dbPath := os.Getenv("SWAMP_DB_PATH")
	if dbPath == "" {
		dbPath = "swamp.db"
	}

	// Default base directory is "assets", not "documents" -- naming the
	// storage path is the only thing this convention was renamed for
	// (see decisions.log); the package/env var identifiers stay as
	// "documents".
	documentsPath := os.Getenv("SWAMP_DOCUMENTS_PATH")
	if documentsPath == "" {
		documentsPath = "assets"
	}

	sqlDB, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close db: %v", err)
		}
	}()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		log.Fatalf("set goose dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	s := store.New(sqlDB)
	documentsStore := documents.NewStore(documentsPath)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "fetch":
			runFetch(s)
			return
		case "stage":
			runStage(s, documentsStore, os.Args[2:])
			return
		case "export":
			runExport(s, documentsStore, os.Args[2:])
			return
		case "import":
			runImport(s, os.Args[2:])
			return
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [fetch|stage|export|import]\n", os.Args[0])
			os.Exit(1)
		}
	}

	syncer := newSyncer(s)
	if _, err := tea.NewProgram(tui.New(s, syncer, documentsStore), tea.WithAltScreen()).Run(); err != nil {
		log.Fatalf("run tui: %v", err)
	}
}

// newSyncer builds a Syncer configured with every supported job board
// source, keyed by the store.Company.Source value each one handles. Each
// client satisfies sync.PostingFetcher directly -- no adapter type is
// needed, since ashby, greenhouse, and lever all return jobboard.Posting
// directly rather than a client-local type (see decisions.log, #57).
func newSyncer(s *store.Store) *sync.Syncer {
	return sync.New(s, map[string]sync.PostingFetcher{
		"ashby":      ashby.NewClient(),
		"greenhouse": greenhouse.NewClient(),
		"lever":      lever.NewClient(),
	})
}

// runImport bulk-creates companies from a YAML seed file (see the seed
// package for its shape). Each entry is validated against its source's
// real API before being saved, so a bad row is reported and skipped
// rather than silently creating a dead company; re-running the same file
// is always safe (store.CreateCompany is idempotent on source+source_ref).
func runImport(s *store.Store, args []string) {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s import <seed-file.yaml>\n", os.Args[0])
		os.Exit(1)
	}

	f, err := os.Open(args[0])
	if err != nil {
		log.Fatalf("open seed file: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("close seed file: %v", err)
		}
	}()

	entries, err := seed.Parse(f)
	if err != nil {
		log.Fatalf("parse seed file: %v", err)
	}

	syncer := newSyncer(s)
	results := syncer.ImportCompanies(context.Background(), entries)

	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("%s (%s/%s): error: %v\n", r.Name, r.Source, r.SourceRef, r.Err)
			continue
		}
		fmt.Printf("%s (%s/%s): imported\n", r.Name, r.Source, r.SourceRef)
	}
}

func runFetch(s *store.Store) {
	ctx := context.Background()

	companies, err := s.ListActiveCompanies(ctx)
	if err != nil {
		log.Fatalf("list active companies: %v", err)
	}
	names := make(map[int64]string, len(companies))
	for _, c := range companies {
		names[c.ID] = c.Name
	}

	syncer := newSyncer(s)
	results, err := syncer.SyncAll(ctx)
	if err != nil {
		log.Fatalf("sync: %v", err)
	}

	for _, r := range results {
		name := names[r.CompanyID]
		if r.Err != nil {
			fmt.Printf("%s: error: %v\n", name, r.Err)
			continue
		}
		fmt.Printf("%s: fetched=%d created=%d updated=%d closed=%d reopened=%d\n",
			name, r.Fetched, r.Created, r.Updated, r.Closed, r.Reopened)
	}
}

// runStage drives the agent hand-off mechanism: `stage list` prints
// eligible postings as a JSON array, `stage prepare <posting-id>` commits
// to one and prints the result as a JSON object. See the stage package
// for what each does.
func runStage(s *store.Store, d *documents.Store, args []string) {
	usage := func() {
		fmt.Fprintf(os.Stderr, "usage: %s stage [list|prepare <posting-id>]\n", os.Args[0])
		os.Exit(1)
	}
	if len(args) == 0 {
		usage()
	}

	st := stage.New(s, d)
	ctx := context.Background()

	switch args[0] {
	case "list":
		candidates, err := st.List(ctx)
		if err != nil {
			log.Fatalf("stage list: %v", err)
		}
		printJSON(candidates)
	case "prepare":
		if len(args) < 2 {
			usage()
		}
		postingID, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			log.Fatalf("invalid posting id %q: %v", args[1], err)
		}
		prepared, err := st.Prepare(ctx, postingID)
		if err != nil {
			log.Fatalf("stage prepare: %v", err)
		}
		printJSON(prepared)
	default:
		usage()
	}
}

// runExport converts an application's existing cover_letter.md/resume.md
// into PDFs alongside them, one sibling <name>.pdf per document that
// exists -- most job boards' file-upload fields don't accept raw
// markdown, and raw markdown syntax in a submission reads unprofessionally
// regardless (see decisions.log, issue #45). Only documents that already
// exist on disk are converted; a document that hasn't been drafted yet is
// reported and skipped, not treated as an error. Exporting doesn't
// require a passed review -- a flagged or unreviewed draft can still be
// useful to preview as a PDF -- but each line reports the latest review
// outcome so the caller can tell a ready-to-submit document from one that
// still needs work.
func runExport(s *store.Store, d *documents.Store, args []string) {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s export <application-id>\n", os.Args[0])
		os.Exit(1)
	}
	applicationID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		log.Fatalf("invalid application id %q: %v", args[0], err)
	}

	ctx := context.Background()
	reviews, err := s.LatestDocumentReviews(ctx, applicationID)
	if err != nil {
		log.Fatalf("export: latest document reviews: %v", err)
	}

	status := d.Status(applicationID)
	reviews, err = currentDocumentReviews(status, reviews)
	if err != nil {
		log.Fatalf("export: current document reviews: %v", err)
	}

	for _, documentType := range []store.DocumentType{store.DocumentTypeCoverLetter, store.DocumentTypeResume} {
		doc := status.CoverLetter
		if documentType == store.DocumentTypeResume {
			doc = status.Resume
		}
		if !doc.Exists {
			fmt.Printf("%s: no document on disk, skipped\n", documentType)
			continue
		}

		outPath, err := exportDocumentPDF(doc.Path)
		if err != nil {
			fmt.Printf("%s: error: %v\n", documentType, err)
			continue
		}
		fmt.Printf("%s: exported to %s (%s)\n", documentType, outPath, reviewSummary(reviews[documentType]))
	}
}

// currentDocumentReviews filters reviews down to only those whose
// content hash still matches each document's actual current content on
// disk -- mirrors stage.currentReviews and tui.currentDocumentReviews
// (see decisions.log, store.DocumentReview.IsCurrent): a review whose
// content has since diverged describes a version of the document that
// no longer exists and must not be surfaced as if it still described
// what's on disk now. Kept as its own local copy rather than shared
// across packages since store deliberately has no filesystem access and
// documents deliberately never reads file content (see documents.go's
// own doc comment), so each caller composes the two itself.
func currentDocumentReviews(status documents.Status, reviews map[store.DocumentType]store.DocumentReview) (map[store.DocumentType]store.DocumentReview, error) {
	out := make(map[store.DocumentType]store.DocumentReview, len(reviews))
	for documentType, review := range reviews {
		doc := status.CoverLetter
		if documentType == store.DocumentTypeResume {
			doc = status.Resume
		}
		if !doc.Exists {
			continue
		}
		content, err := os.ReadFile(doc.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", doc.Path, err)
		}
		if !review.IsCurrent(string(content)) {
			continue
		}
		out[documentType] = review
	}
	return out, nil
}

// exportDocumentPDF renders mdPath's markdown content to a sibling .pdf
// file (same directory, extension swapped) via the export package, and
// returns its path -- the CLI's fixed destination convention, unlike the
// TUI's export screen, where the user picks the directory.
func exportDocumentPDF(mdPath string) (string, error) {
	outPath := strings.TrimSuffix(mdPath, filepath.Ext(mdPath)) + ".pdf"
	if err := export.Document(mdPath, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

// reviewSummary describes review's outcome for the export CLI's output --
// review is the zero value when the document has no recorded review yet
// (store.LatestDocumentReviews omits any document type it has no review
// for), which is a real, distinct state from either outcome and worth
// saying so explicitly rather than defaulting to one.
func reviewSummary(review store.DocumentReview) string {
	if review.CreatedAt.IsZero() {
		return "not yet reviewed"
	}
	if review.Outcome == store.ReviewOutcomeFlagged {
		return "flagged: " + review.Notes
	}
	return "passed"
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Fatalf("encode json: %v", err)
	}
}
