// Command swamp is the entrypoint: it launches the TUI by default, or
// runs a one-off refresh with the `fetch` subcommand, or drives the agent
// hand-off mechanism with the `stage` subcommand. Not unit tested per
// this project's testing decisions -- verified manually.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/ashby"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
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

	documentsPath := os.Getenv("SWAMP_DOCUMENTS_PATH")
	if documentsPath == "" {
		documentsPath = "documents"
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
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [fetch|stage]\n", os.Args[0])
			os.Exit(1)
		}
	}

	syncer := sync.New(s, ashby.NewClient())
	if _, err := tea.NewProgram(tui.New(s, syncer, documentsStore), tea.WithAltScreen()).Run(); err != nil {
		log.Fatalf("run tui: %v", err)
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

	syncer := sync.New(s, ashby.NewClient())
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

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Fatalf("encode json: %v", err)
	}
}
