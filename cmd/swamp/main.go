// Command swamp is the entrypoint: it launches the TUI by default, applies
// or rolls back database migrations with `migrate up` and `migrate down`
// (and only then), runs a
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
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/export"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/jobboard/ashby"
	"github.com/dklassen/swamp/jobboard/greenhouse"
	"github.com/dklassen/swamp/jobboard/lever"
	"github.com/dklassen/swamp/seed"
	"github.com/dklassen/swamp/stage"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui"
	"github.com/dklassen/swamp/tui/model"
)

// main alone picks the exit status, after run's deferred closes have run.
func main() {
	err := run(os.Args[1:])
	var usageErr usageError
	switch {
	case err == nil:
	case errors.As(err, &usageErr):
		fmt.Fprintf(os.Stderr, "swamp: %v\n%s\n", usageErr.err, usage)
		os.Exit(2)
	case errors.Is(err, errFailuresReported):
		os.Exit(1)
	default:
		log.Print(err)
		os.Exit(1)
	}
}

// errFailuresReported: the command has already said what failed, so it
// only sets the exit status.
var errFailuresReported = errors.New("failures reported")

// usageError is arguments swamp can't run, which the usage text explains.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func run(args []string) error {
	rf, err := parseArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Println(usage)
		return nil
	}
	if err != nil {
		return usageError{err}
	}
	return runCommand(rf)
}

func runCommand(rf runFlags) error {
	dbPath := os.Getenv("SWAMP_DB_PATH")
	if dbPath == "" {
		dbPath = "swamp.db"
	}

	// Only the directory was renamed; the package and env var stay "documents".
	documentsPath := os.Getenv("SWAMP_DOCUMENTS_PATH")
	if documentsPath == "" {
		documentsPath = "assets"
	}

	if rf.command == cmdMigrate {
		return runMigrate(dbPath, rf.migrate)
	}
	// On a plain handle: with an origin, an out-of-date database would fail
	// in the connection hook instead, with a less useful error.
	if err := checkSchema(dbPath); err != nil {
		return err
	}

	// Other processes read the origin to tell who made a change.
	cfg := store.DefaultConfig()
	cfg.Origin = fmt.Sprintf("%s:%d", rf.kind(), os.Getpid())
	sqlDB, err := store.Open(dbPath, cfg)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close db: %v", err)
		}
	}()

	db := store.New(sqlDB)
	documentsStore := documents.NewStore(documentsPath)

	switch rf.command {
	case cmdFetch:
		return runFetch(db)
	case cmdStage:
		return runStage(db, documentsStore, rf.stage, rf.postingID)
	case cmdExport:
		return runExport(db, documentsStore, rf.applicationID)
	case cmdImport:
		return runImport(db, rf.seedFile)
	case cmdMCPServe:
		return runMCPServe(db, documentsStore)
	case cmdTUI, cmdMigrate:
		// Migrate returned before the database opened; the TUI runs below.
	}
	return runTUI(db, documentsStore, dbPath, cfg.Origin, rf.mcp)
}

// runTUI runs the TUI on db as origin, and with withMCP the MCP server
// alongside it, until the user quits.
func runTUI(db store.Store, documentsStore *documents.Store, dbPath, origin string, withMCP bool) error {
	logPath := os.Getenv("SWAMP_LOG_PATH")
	if logPath == "" {
		logPath = filepath.Join(filepath.Dir(dbPath), "swamp.log")
	}
	logger, closeLog, err := openLog(logPath)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer func() { _ = closeLog() }()

	syncer := newSyncer(db)
	// Read before the TUI's first loads, which already show anything older.
	changes := service.NewChangeLog(db)
	since, err := changes.Latest(context.Background())
	if err != nil {
		return fmt.Errorf("read the change log: %w", err)
	}
	companies := service.NewCompanyService(db)
	postings := service.NewPostingService(db, nil, service.FormConfig{})
	applications := service.NewApplicationService(db, documentsStore)
	documentService := service.NewDocumentService(db, documentsStore)
	scores := service.NewScoringService(db)
	router := model.NewMsgRouter()
	screens := tui.NewScreens(companies, postings, applications, documentService, scores, syncer, documentsStore)
	if err := router.Register(screens); err != nil {
		return fmt.Errorf("register the screens: %w", err)
	}
	reader := tui.NewChangeReader(companies, postings, applications, documentService, scores, changes, origin, since, tui.ChangePollInterval)
	app := tui.New(
		router,
		tui.NewNavigation(screens.Tabs()),
		tui.NewStack(),
		screens.Tables(),
		tui.WithSyncer(syncer),
		tui.WithChangeReader(reader),
	)
	program := tea.NewProgram(app)
	stopMCP := func(context.Context) error { return nil }
	if withMCP {
		// About to crash anyway: a failed release has nowhere better to go.
		release := func() { _ = program.ReleaseTerminal() }
		addr := mcpAddr()
		stopMCP, err = mcpForTUI(addr, dbPath, documentsStore, logger.With("component", "mcp"), release)
		if err != nil {
			return fmt.Errorf("swamp --mcp: %w", err)
		}
		app.WithStatus("MCP server on " + addr)
	}

	// While the TUI owns the terminal, anything written with log would
	// garble it: send it to the log file instead.
	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(logger.With("component", "tui").Handler(), slog.LevelInfo).Writer())
	_, runErr := program.Run()
	log.SetFlags(log.LstdFlags)
	log.SetOutput(os.Stderr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := stopMCP(ctx); err != nil {
		log.Printf("stop the MCP server: %v", err)
	}
	cancel()
	// Quitting abandons a refresh or sync-all still in flight, whose own
	// lease release would then never run: free its lease before the
	// database closes, or the next sync of that company is refused until
	// the lease expires (#153).
	if err := syncer.ReleaseHeldLeases(context.Background()); err != nil {
		log.Printf("release sync leases: %v", err)
	}
	if runErr != nil {
		return fmt.Errorf("run tui: %w", runErr)
	}
	return nil
}

// newSyncer builds a Syncer configured with every supported job board
// source, keyed by the company.Company.Source value each one handles. Each
// client satisfies sync.PostingFetcher directly -- no adapter type is
// needed, since ashby, greenhouse, and lever all return jobboard.Posting
// directly rather than a client-local type (#57).
func newSyncer(db store.Store) *sync.Syncer {
	return sync.New(service.NewCompanyService(db), service.NewPostingService(db, nil, service.FormConfig{}), map[string]sync.PostingFetcher{
		jobboard.Ashby:      ashby.NewClient(),
		jobboard.Greenhouse: greenhouse.NewClient(),
		jobboard.Lever:      lever.NewClient(),
	}, sync.DefaultConfig())
}

// newStage builds the agent hand-off Stage, able to read application
// forms from every board that supports it (Greenhouse; see #167 for Ashby
// and Lever) under the same fetch timeout as a sync.
func newStage(db store.Store, d *documents.Store) *stage.Stage {
	postings := service.NewPostingService(db, map[string]service.FormFetcher{
		jobboard.Greenhouse: greenhouse.NewClient(),
	}, service.FormConfig{FetchTimeout: sync.DefaultConfig().FetchTimeout})
	return stage.New(service.NewCompanyService(db), postings, service.NewApplicationService(db, d), service.NewDocumentService(db, d), service.NewScoringService(db), d)
}

// runImport bulk-creates companies from a YAML seed file (see the seed
// package for its shape). Each entry is validated against its source's
// real API before being saved, so a bad row is reported and skipped
// rather than silently creating a dead company; re-running the same file
// is always safe (CompanyService.Add is idempotent on source+source_ref).
func runImport(db store.Store, seedFile string) error {
	f, err := os.Open(seedFile)
	if err != nil {
		return fmt.Errorf("open seed file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("close seed file: %v", err)
		}
	}()

	entries, err := seed.Parse(f)
	if err != nil {
		return fmt.Errorf("parse seed file: %w", err)
	}

	syncer := newSyncer(db)
	results := syncer.ImportCompanies(context.Background(), entries)

	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("%s (%s/%s): error: %v\n", r.Name, r.Source, r.SourceRef, r.Err)
			continue
		}
		fmt.Printf("%s (%s/%s): imported\n", r.Name, r.Source, r.SourceRef)
	}
	return nil
}

// runFetch syncs every active company and fails if any did, so swamp exits
// non-zero when something scheduled it and nobody is watching (issue #145).
func runFetch(db store.Store) error {
	syncer := newSyncer(db)
	results, err := syncer.SyncAll(context.Background())
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	if reportFetch(os.Stdout, os.Stderr, results) > 0 {
		return errFailuresReported
	}
	return nil
}

// reportFetch prints one line per company -- successes to stdout, errors
// and skips to stderr -- then sync.Summarize's one-line summary to
// stderr, e.g. "41 companies, 1 failed (Vandelay)", and returns how many
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
func runStage(db store.Store, d *documents.Store, action stageAction, postingID int64) error {
	st := newStage(db, d)
	ctx := context.Background()

	switch action {
	case stageList:
		candidates, err := st.List(ctx)
		if err != nil {
			return fmt.Errorf("stage list: %w", err)
		}
		return printJSON(candidates)
	case stagePrepare:
		prepared, err := st.Prepare(ctx, postingID)
		if err != nil {
			return fmt.Errorf("stage prepare: %w", err)
		}
		return printJSON(prepared)
	default:
		return fmt.Errorf("stage: unknown action %q", action)
	}
}

// runMCPServe runs swamp's agent hand-off mechanism as an MCP server over
// the Streamable HTTP transport rather than one-off CLI calls -- needed
// when the caller (an MCP-capable Claude host) is running somewhere that
// can't spawn or reach the swamp binary directly, e.g. inside a
// container.
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
// running swamp directly; add a bearer-token check
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
func runMCPServe(db store.Store, d *documents.Store) error {
	addr := mcpAddr()
	handler := mcpHandler(db, d, newSyncer(db))

	log.Printf("swamp mcp-serve: listening on %s", addr)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: readHeaderTimeout}
	if err := srv.ListenAndServe(); err != nil {
		return fmt.Errorf("mcp-serve: %w", err)
	}
	return nil
}

// runExport converts an application's existing cover_letter.md/resume.md
// into PDFs alongside them, one sibling <name>.pdf per document that
// exists -- most job boards' file-upload fields don't accept raw
// markdown, and raw markdown syntax in a submission reads unprofessionally
// regardless (issue #45). Only documents that already
// exist on disk are converted; a document that hasn't been drafted yet is
// reported and skipped, not treated as an error. Exporting doesn't
// require a passed review -- a flagged or unreviewed draft can still be
// useful to preview as a PDF -- but each line reports the latest review
// outcome so the caller can tell a ready-to-submit document from one that
// still needs work.
func runExport(db store.Store, d *documents.Store, applicationID int64) error {
	ctx := context.Background()
	// Validate the ID against a real row before anything else. Without
	// this, a typo'd or stale ID walks the same path as a real
	// application with nothing drafted yet -- empty reviews, a Status
	// with Exists false for both documents -- and prints the identical
	// "no document on disk, skipped" lines while exiting 0 (see
	// issue #102).
	if _, err := service.NewApplicationService(db, d).Get(ctx, applicationID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("export: no application with id %d", applicationID)
		}
		return fmt.Errorf("export: look up application %d: %w", applicationID, err)
	}

	documentService := service.NewDocumentService(db, d)
	reviews, err := documentService.LatestReviews(ctx, applicationID)
	if err != nil {
		return fmt.Errorf("export: latest document reviews: %w", err)
	}

	status := d.Status(applicationID)
	for _, documentType := range document.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			fmt.Printf("%s: error: %v\n", documentType, err)
			continue
		}
		if !doc.Exists {
			fmt.Printf("%s: no document on disk, skipped\n", documentType)
			continue
		}

		outPath, err := exportDocumentPDF(ctx, documentService, applicationID, documentType, doc.Path)
		if err != nil {
			fmt.Printf("%s: error: %v\n", documentType, err)
			continue
		}
		review, hasReview := reviews[documentType]
		fmt.Printf("%s: exported to %s (%s)\n", documentType, outPath, reviewSummary(review, hasReview))
	}
	return nil
}

// exportDocumentPDF renders mdPath's markdown content to a sibling .pdf
// file (same directory, extension swapped) via the export package, and
// returns its path -- the CLI's fixed destination convention, unlike the
// TUI's export screen, where the user picks the directory. The export is
// recorded against applicationID with the content it was rendered from
// (DocumentService.RecordExport), as the TUI's are.
func exportDocumentPDF(ctx context.Context, documentService *service.DocumentService, applicationID int64, documentType document.Type, mdPath string) (string, error) {
	outPath := strings.TrimSuffix(mdPath, filepath.Ext(mdPath)) + ".pdf"
	content, err := export.Document(mdPath, outPath)
	if err != nil {
		return "", err
	}
	if err := documentService.RecordExport(ctx, applicationID, documentType, content, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

// reviewSummary describes review's outcome for the export CLI's output.
// hasReview is whether the review map had an entry for the document: no
// current review is a real, distinct state from either outcome and worth
// saying so explicitly rather than defaulting to one. It's never inferred
// from review's fields (RFC 0005).
func reviewSummary(review document.Review, hasReview bool) string {
	if !hasReview {
		return "not yet reviewed"
	}
	if review.Outcome == document.Flagged {
		return "flagged: " + review.Notes
	}
	return "passed"
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

func openPlain(dbPath string) (*sql.DB, error) {
	sqlDB, err := store.Open(dbPath, store.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return sqlDB, nil
}

func checkSchema(dbPath string) error {
	sqlDB, err := openPlain(dbPath)
	if err != nil {
		return err
	}
	err = migrations.Check(context.Background(), sqlDB)
	if closeErr := sqlDB.Close(); closeErr != nil {
		log.Printf("close db: %v", closeErr)
	}
	return err
}

// runMigrate applies the database migrations this binary has and the
// database doesn't (up), or rolls back the newest one (down), and says
// what it did.
func runMigrate(dbPath string, direction migrateDirection) error {
	sqlDB, err := openPlain(dbPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close db: %v", err)
		}
	}()
	ctx := context.Background()

	if direction == migrateDown {
		version, err := migrations.Down(ctx, sqlDB)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		fmt.Printf("Rolled back migration %d. Until `swamp migrate up`, the other commands refuse this database.\n", version)
		return nil
	}

	applied, err := migrations.Up(ctx, sqlDB)
	if err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	if len(applied) == 0 {
		fmt.Printf("The database is already at version %d.\n", migrations.Latest())
		return nil
	}
	fmt.Printf("Applied %d migration(s), %d to %d. The database is now at version %d.\n", len(applied), applied[0], applied[len(applied)-1], migrations.Latest())
	return nil
}
