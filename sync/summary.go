package sync

import (
	"errors"
	"fmt"
	"strings"
)

// Summary is the outcome of syncing several companies, as one line:
// `swamp fetch` prints it and the TUI's sync-all shows it (#151).
type Summary struct {
	Companies int
	// Failed names the companies whose sync failed, in result order.
	Failed []string
	// Skipped names the companies another sync was already running for
	// (ErrSyncInProgress, #150), in result order. Not failures: they're
	// being synced, just not by this run.
	Skipped []string
}

// Summarize tallies results into a Summary.
func Summarize(results []Result) Summary {
	summary := Summary{Companies: len(results)}
	for _, r := range results {
		switch {
		case errors.Is(r.Err, ErrSyncInProgress):
			summary.Skipped = append(summary.Skipped, r.Name)
		case r.Err != nil:
			summary.Failed = append(summary.Failed, r.Name)
		}
	}
	return summary
}

// String renders the summary, e.g. "40 companies, 1 failed (Kong)". The
// skipped count is added only when there is one, so a clean run's line --
// which logs and schedulers read -- stays the same.
func (s Summary) String() string {
	noun := "companies"
	if s.Companies == 1 {
		noun = "company"
	}
	line := fmt.Sprintf("%d %s, %d failed", s.Companies, noun, len(s.Failed))
	if len(s.Failed) > 0 {
		line += " (" + strings.Join(s.Failed, ", ") + ")"
	}
	if len(s.Skipped) > 0 {
		line += fmt.Sprintf(", %d skipped (%s)", len(s.Skipped), strings.Join(s.Skipped, ", "))
	}
	return line
}
