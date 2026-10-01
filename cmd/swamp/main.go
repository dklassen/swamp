// Command swamp is the entrypoint: it launches the TUI by default, runs a
// one-off refresh with the `fetch` subcommand, drives the agent hand-off
// mechanism with the `stage` subcommand, converts an application's
// drafted documents to PDF with the `export` subcommand, or bulk-creates
// companies from a YAML seed file with the `import` subcommand. Mostly not
// unit tested per this project's testing decisions -- verified manually.
// The exception is fetch's output (reportFetch), since scripts and
// schedulers depend on its exit status and summary (issue #145).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/ashby"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/export"
	"github.com/dklassen/swamp/greenhouse"
	"github.com/dklassen/swamp/lever"
	"github.com/dklassen/swamp/mcpserver"
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

	sqlDB, err := store.Open(dbPath, store.DefaultConfig())
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
			if runFetch(s) > 0 {
				// os.Exit skips the deferred Close, so close first to
				// leave the database checkpointed.
				if err := sqlDB.Close(); err != nil {
					log.Printf("close db: %v", err)
				}
				os.Exit(1)
			}
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
		case "mcp-serve":
			runMCPServe(s, documentsStore)
			return
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [fetch|stage|export|import|mcp-serve]\n", os.Args[0])
			os.Exit(1)
		}
	}

	syncer := newSyncer(s)
	_, runErr := tea.NewProgram(tui.New(s, syncer, documentsStore), tea.WithAltScreen()).Run()
	// Quitting abandons a refresh or sync-all still in flight, whose own
	// lease release would then never run: free its lease before the
	// database closes, or the next sync of that company is refused until
	// the lease expires (#153).
	if err := syncer.ReleaseHeldLeases(context.Background()); err != nil {
		log.Printf("release sync leases: %v", err)
	}
	if runErr != nil {
		log.Fatalf("run tui: %v", runErr)
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
	}, sync.DefaultConfig())
}

// newStage builds the agent hand-off Stage, able to read application
// forms from every board that supports it (Greenhouse; see #167 for Ashby
// and Lever) under the same fetch timeout as a sync.
func newStage(s *store.Store, d *documents.Store) *stage.Stage {
	return stage.New(s, d, stage.WithFormFetchers(map[string]stage.FormFetcher{
		"greenhouse": greenhouse.NewClient(),
	}, sync.DefaultConfig().FetchTimeout))
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

// runFetch syncs every active company and reports how many failed, so main
// can exit non-zero when something scheduled it and nobody is watching
// (issue #145).
func runFetch(s *store.Store) int {
	syncer := newSyncer(s)
	results, err := syncer.SyncAll(context.Background())
	if err != nil {
		log.Fatalf("sync: %v", err)
	}

	return reportFetch(os.Stdout, os.Stderr, results)
}

// reportFetch prints one line per company -- successes to stdout, errors
// and skips to stderr -- then sync.Summarize's one-line summary to
// stderr, e.g. "41 companies, 1 failed (Outschool)", and returns how many
// companies failed. A company skipped because another sync of it was
// already running (sync.ErrSyncInProgress, #150) isn't a failure: it's
// being synced, just not by this run.
func reportFetch(stdout, stderr io.Writer, results []sync.Result) int {
	for _, r := range results {
		switch {
		case errors.Is(r.Err, sync.ErrSyncInProgress):
			_, _ = fmt.Fprintf(stderr, "%s: skipped, another sync of it is in progress\n", r.Name)
		case r.Err != nil:
			_, _ = fmt.Fprintf(stderr, "%s: error: %v\n", r.Name, r.Err)
		default:
			_, _ = fmt.Fprintf(stdout, "%s: fetched=%d created=%d updated=%d closed=%d reopened=%d\n",
				r.Name, r.Fetched, r.Created, r.Updated, r.Closed, r.Reopened)
		}
	}

	summary := sync.Summarize(results)
	_, _ = fmt.Fprintln(stderr, summary)
	return len(summary.Failed)
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

	st := newStage(s, d)
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

// runMCPServe runs swamp's agent hand-off mechanism as an MCP server over
// the Streamable HTTP transport rather than one-off CLI calls -- needed
// when the caller (an MCP-capable Claude host) is running somewhere that
// can't spawn or reach the swamp binary directly, e.g. inside a
// container. See decisions.log for why MCP/Streamable-HTTP specifically,
// rather than gRPC or a plain REST API, is the right fit here.
//
// Binds to SWAMP_MCP_ADDR (default "127.0.0.1:8787"). 127.0.0.1 is
// correct, not just safe, for Apple's `container` framework: its
// host.container.internal DNS entry is implemented as a redirect-to-
// localhost on the host side (see `container system dns create --help`'s
// --localhost flag), verified directly by logging http.Request's
// LocalAddrContextKey for a real container request -- RemoteAddr showed
// the container's real vmnet address, but LocalAddr was 127.0.0.1
// regardless. So the traffic that matters here never actually arrives on
// any other host interface; binding wider than loopback would just
// expose this on the LAN for no reachability benefit. No auth on this
// endpoint for now -- single-user local dev machine, same trust level as
// running swamp directly (see decisions.log); add a bearer-token check
// before this is ever reachable beyond this Mac.
//
// DisableLocalhostProtection is set because the SDK's default DNS-rebinding
// protection rejects any request whose local address is loopback but whose
// Host header isn't a recognized localhost value -- which every
// host.container.internal request is, by the mechanism above. Confirmed
// there's no way around this by choosing a different bind address: the
// redirect targets loopback specifically, so the protection's loopback
// check will always fire for this traffic. Safe to disable here for the
// same reason auth is skipped: single-user local dev machine, deliberately
// reached by a known local container over a known DNS name, not the
// untrusted-browser threat this protection exists for.
func runMCPServe(s *store.Store, d *documents.Store) {
	addr := os.Getenv("SWAMP_MCP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}

	st := newStage(s, d)
	server := mcpserver.New(st, d, newSyncer(s))

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true})

	log.Printf("swamp mcp-serve: listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("mcp-serve: %v", err)
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
	// Validate the ID against a real row before anything else. Without
	// this, a typo'd or stale ID walks the same path as a real
	// application with nothing drafted yet -- empty reviews, a Status
	// with Exists false for both documents -- and prints the identical
	// "no document on disk, skipped" lines while exiting 0 (see
	// decisions.log, issue #102).
	if _, err := s.GetApplicationByID(ctx, applicationID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			log.Fatalf("export: no application with id %d", applicationID)
		}
		log.Fatalf("export: look up application %d: %v", applicationID, err)
	}

	reviews, err := s.LatestDocumentReviews(ctx, applicationID)
	if err != nil {
		log.Fatalf("export: latest document reviews: %v", err)
	}

	status := d.Status(applicationID)
	reviews, err = documents.Current(status, reviews, store.DocumentReview.IsCurrent)
	if err != nil {
		log.Fatalf("export: current document reviews: %v", err)
	}

	for _, documentType := range documents.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			fmt.Printf("%s: error: %v\n", documentType, err)
			continue
		}
		if !doc.Exists {
			fmt.Printf("%s: no document on disk, skipped\n", documentType)
			continue
		}

		outPath, err := exportDocumentPDF(ctx, s, applicationID, documentType, doc.Path)
		if err != nil {
			fmt.Printf("%s: error: %v\n", documentType, err)
			continue
		}
		review, hasReview := reviews[documentType]
		fmt.Printf("%s: exported to %s (%s)\n", documentType, outPath, reviewSummary(review, hasReview))
	}
}

// exportDocumentPDF renders mdPath's markdown content to a sibling .pdf
// file (same directory, extension swapped) via the export package, and
// returns its path -- the CLI's fixed destination convention, unlike the
// TUI's export screen, where the user picks the directory. The export is
// recorded against applicationID with the content it was rendered from
// (store.RecordDocumentExport, #188), as the TUI's are.
func exportDocumentPDF(ctx context.Context, s *store.Store, applicationID int64, documentType documents.Type, mdPath string) (string, error) {
	outPath := strings.TrimSuffix(mdPath, filepath.Ext(mdPath)) + ".pdf"
	content, err := export.Document(mdPath, outPath)
	if err != nil {
		return "", err
	}
	if err := s.RecordDocumentExport(ctx, applicationID, documentType, content, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

// reviewSummary describes review's outcome for the export CLI's output.
// hasReview is whether the review map had an entry for the document: no
// current review is a real, distinct state from either outcome and worth
// saying so explicitly rather than defaulting to one. It's never inferred
// from review's fields (RFC 0005).
func reviewSummary(review store.DocumentReview, hasReview bool) string {
	if !hasReview {
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
