// Command swamp is the entrypoint: it launches the TUI by default, or
// runs a one-off refresh with the `fetch` subcommand. Not unit tested per
// this project's testing decisions -- verified manually.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/ashby"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui"
)

func main() {
	dbPath := os.Getenv("SWAMP_DB_PATH")
	if dbPath == "" {
		dbPath = "swamp.db"
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

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "fetch":
			runFetch(s)
			return
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [fetch]\n", os.Args[0])
			os.Exit(1)
		}
	}

	syncer := sync.New(s, ashby.NewClient())
	if _, err := tea.NewProgram(tui.New(s, syncer), tea.WithAltScreen()).Run(); err != nil {
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
