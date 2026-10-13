package stage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

const (
	defaultRunsLimit = 10
	maxRunsLimit     = 50
)

// ScoringRun is a scoring run as the agent sees it, with how far it has
// got.
type ScoringRun struct {
	ID            int64     `json:"ID"`
	Model         string    `json:"Model"`
	ClientName    string    `json:"ClientName"`
	ClientVersion string    `json:"ClientVersion"`
	ProfileSHA256 string    `json:"ProfileSHA256"`
	Scope         string    `json:"Scope"`
	StartedAt     time.Time `json:"StartedAt"`
	Scored        int       `json:"Scored"`
	Pending       int       `json:"Pending"`
}

// ScoringRuns is list_scoring_runs' result.
type ScoringRuns struct {
	Runs []ScoringRun `json:"Runs"`
}

func scoringRun(r store.ScoringRunSummary) ScoringRun {
	return ScoringRun{
		ID:            r.ID,
		Model:         r.Model,
		ClientName:    r.ClientName,
		ClientVersion: r.ClientVersion,
		ProfileSHA256: r.ProfileSHA256,
		Scope:         r.Scope.String(),
		StartedAt:     r.StartedAt,
		Scored:        r.Scored,
		Pending:       r.Pending,
	}
}

// StartRunOptions is what start_scoring_run is given, and the client it
// was called from, which may be empty.
type StartRunOptions struct {
	Model         string
	ProfileSHA256 string
	// Scope is a store.ScoringScope name; empty is unscored.
	Scope         string
	ClientName    string
	ClientVersion string
}

// StartScoringRun starts a run against the profile the agent read. It
// refuses a hash that isn't profile.md's now, so a run can't claim a
// profile the agent didn't score against.
func (st *Stage) StartScoringRun(ctx context.Context, opts StartRunOptions) (ScoringRun, error) {
	scope := store.ScoringScopeUnscored
	if opts.Scope != "" {
		var err error
		if scope, err = store.ParseScoringScope(opts.Scope); err != nil {
			return ScoringRun{}, fmt.Errorf("stage: %w", err)
		}
	}
	path := st.documents.ProfilePath()
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ScoringRun{}, fmt.Errorf("stage: there's no profile at %s to score against", path)
	}
	if err != nil {
		return ScoringRun{}, fmt.Errorf("stage: read profile: %w", err)
	}
	if current := documents.ContentSHA256(string(content)); opts.ProfileSHA256 != current {
		return ScoringRun{}, fmt.Errorf("stage: the profile isn't the one whose SHA256 you gave (it changed, or wasn't read): call read_profile and pass its SHA256")
	}
	run, err := st.store.StartScoringRun(ctx, store.StartScoringRunParams{
		Model:         opts.Model,
		ClientName:    opts.ClientName,
		ClientVersion: opts.ClientVersion,
		ProfileSHA256: opts.ProfileSHA256,
		Scope:         scope,
	})
	if err != nil {
		return ScoringRun{}, fmt.Errorf("stage: %w", err)
	}
	return scoringRun(run), nil
}

// ScoringRuns returns the most recent runs, newest first: defaultRunsLimit
// when limit is 0 or less, never more than maxRunsLimit.
func (st *Stage) ScoringRuns(ctx context.Context, limit int) (ScoringRuns, error) {
	if limit <= 0 {
		limit = defaultRunsLimit
	}
	runs, err := st.store.ScoringRuns(ctx, min(limit, maxRunsLimit))
	if err != nil {
		return ScoringRuns{}, fmt.Errorf("stage: %w", err)
	}
	result := ScoringRuns{Runs: make([]ScoringRun, len(runs))}
	for i, r := range runs {
		result.Runs[i] = scoringRun(r)
	}
	return result, nil
}
