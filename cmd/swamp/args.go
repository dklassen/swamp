package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
)

type command string

const (
	cmdTUI      command = "tui"
	cmdMigrate  command = "migrate"
	cmdFetch    command = "fetch"
	cmdStage    command = "stage"
	cmdExport   command = "export"
	cmdImport   command = "import"
	cmdMCPServe command = "mcp-serve"
)

type migrateDirection string

const (
	migrateUp   migrateDirection = "up"
	migrateDown migrateDirection = "down"
)

type stageAction string

const (
	stageList    stageAction = "list"
	stagePrepare stageAction = "prepare"
)

const usage = `usage:
  swamp [--mcp]                  run the TUI, optionally with the MCP server in-process
  swamp migrate up|down          apply pending migrations, or roll back the newest
  swamp fetch                    sync every active company
  swamp stage list               print postings eligible for hand-off
  swamp stage prepare <posting-id>
  swamp export <application-id>  convert an application's documents to PDF
  swamp import <seed-file.yaml>  bulk-create companies
  swamp mcp-serve                run the MCP server alone`

// runFlags is the parsed command line.
type runFlags struct {
	command command
	// mcp runs the MCP server inside the TUI process.
	mcp           bool
	migrate       migrateDirection
	stage         stageAction
	postingID     int64
	applicationID int64
	seedFile      string
}

// parseArgs parses args, os.Args without the program name. A usage
// mistake is an error to print with usage; -h is flag.ErrHelp.
func parseArgs(args []string) (runFlags, error) {
	var f runFlags
	top := newFlagSet("swamp")
	top.BoolVar(&f.mcp, "mcp", false, "")
	if err := top.Parse(args); err != nil {
		return runFlags{}, err
	}
	if top.NArg() == 0 {
		f.command = cmdTUI
		return f, nil
	}
	f.command = command(top.Arg(0))
	if f.mcp {
		return runFlags{}, fmt.Errorf("--mcp only applies to the TUI, not %s", f.command)
	}

	sub := newFlagSet(string(f.command))
	if err := sub.Parse(top.Args()[1:]); err != nil {
		return runFlags{}, err
	}
	var err error
	switch f.command {
	case cmdFetch, cmdMCPServe:
		err = wantArgs(sub, 0)
	case cmdMigrate:
		f.migrate = migrateDirection(sub.Arg(0))
		if sub.NArg() != 1 || (f.migrate != migrateUp && f.migrate != migrateDown) {
			err = fmt.Errorf("migrate: want up or down, got %q", sub.Args())
		}
	case cmdStage:
		err = parseStage(sub, &f)
	case cmdExport:
		if err = wantArgs(sub, 1); err == nil {
			f.applicationID, err = parseID("application", sub.Arg(0))
		}
	case cmdImport:
		if err = wantArgs(sub, 1); err == nil {
			f.seedFile = sub.Arg(0)
		}
	default:
		err = fmt.Errorf("unknown command %q", f.command)
	}
	if err != nil {
		return runFlags{}, err
	}
	return f, nil
}

func parseStage(sub *flag.FlagSet, f *runFlags) error {
	if sub.NArg() == 0 {
		return errors.New("stage: want list or prepare")
	}
	f.stage = stageAction(sub.Arg(0))
	switch f.stage {
	case stageList:
		return wantArgs(sub, 1)
	case stagePrepare:
		if err := wantArgs(sub, 2); err != nil {
			return err
		}
		var err error
		f.postingID, err = parseID("posting", sub.Arg(1))
		return err
	}
	return fmt.Errorf("stage: want list or prepare, got %q", sub.Arg(0))
}

// newFlagSet returns errors for the caller to print instead of printing
// them itself, so parseArgs stays testable.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func wantArgs(fs *flag.FlagSet, n int) error {
	if fs.NArg() != n {
		return fmt.Errorf("%s: want %d argument(s), got %d: %q", fs.Name(), n, fs.NArg(), fs.Args())
	}
	return nil
}

func parseID(what, s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s id %q", what, s)
	}
	return id, nil
}

// kind is the first half of this process's change-log origin.
func (f runFlags) kind() string {
	if f.command == cmdMCPServe {
		return "mcp"
	}
	return string(f.command)
}
