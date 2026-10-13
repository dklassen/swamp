package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// ScoringScope is which postings a scoring run covers.
type ScoringScope int

const (
	// ScoringScopeUnscored: postings with no score on their current
	// content, so new and changed ones.
	ScoringScopeUnscored ScoringScope = iota
	// ScoringScopeAll: every eligible posting, for a rescore after a
	// profile change or with another model.
	ScoringScopeAll
)

// scoringScopeNames is the single name table for ScoringScope, indexed by
// its value; scoring_runs.scope holds these, with no CHECK (00028).
var scoringScopeNames = [...]string{
	ScoringScopeUnscored: "unscored",
	ScoringScopeAll:      "all",
}

func (s ScoringScope) String() string {
	if s < 0 || int(s) >= len(scoringScopeNames) {
		return fmt.Sprintf("ScoringScope(%d)", int(s))
	}
	return scoringScopeNames[s]
}

// ParseScoringScope is the scope named s, or an error for any other name.
func ParseScoringScope(s string) (ScoringScope, error) {
	for i, name := range scoringScopeNames {
		if name == s {
			return ScoringScope(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown scoring scope %q: want unscored or all", s)
}

// ScoringScopes returns every scope, in const order.
func ScoringScopes() []ScoringScope {
	scopes := make([]ScoringScope, len(scoringScopeNames))
	for i := range scoringScopeNames {
		scopes[i] = ScoringScope(i)
	}
	return scopes
}

// ScoringRun is one scoring session the agent started.
type ScoringRun struct {
	ID            int64
	Model         string
	ClientName    string
	ClientVersion string
	ProfileSHA256 string
	Scope         ScoringScope
	StartedAt     time.Time
}

func scoringRunFromRow(row db.ScoringRun) (ScoringRun, error) {
	scope, err := ParseScoringScope(row.Scope)
	if err != nil {
		return ScoringRun{}, fmt.Errorf("store: scoring run %d: %w", row.ID, err)
	}
	return ScoringRun{
		ID:            row.ID,
		Model:         row.Model,
		ClientName:    row.ClientName,
		ClientVersion: row.ClientVersion,
		ProfileSHA256: row.ProfileSha256,
		Scope:         scope,
		StartedAt:     row.StartedAt,
	}, nil
}

// StartScoringRunParams is what a scoring run records about itself. The
// client's name and version may be empty: MCP client info is optional.
type StartScoringRunParams struct {
	Model         string
	ClientName    string
	ClientVersion string
	ProfileSHA256 string
	Scope         ScoringScope
}

// ScoringRunSummary is a run with how far it has got, when it was read.
type ScoringRunSummary struct {
	ScoringRun
	Scored  int
	Pending int
}

// StartScoringRun records a new scoring run, and says how many postings
// it has to score.
func (s *Store) StartScoringRun(ctx context.Context, params StartScoringRunParams) (ScoringRunSummary, error) {
	switch {
	case params.Model == "":
		return ScoringRunSummary{}, errors.New("store: start scoring run: model is required")
	case params.ProfileSHA256 == "":
		return ScoringRunSummary{}, errors.New("store: start scoring run: profile hash is required")
	case params.Scope < 0 || int(params.Scope) >= len(scoringScopeNames):
		return ScoringRunSummary{}, fmt.Errorf("store: start scoring run: unknown scope %v", params.Scope)
	}
	row, err := s.queries.CreateScoringRun(ctx, db.CreateScoringRunParams{
		Model:         params.Model,
		ClientName:    params.ClientName,
		ClientVersion: params.ClientVersion,
		ProfileSha256: params.ProfileSHA256,
		Scope:         params.Scope.String(),
	})
	if err != nil {
		return ScoringRunSummary{}, fmt.Errorf("store: start scoring run: %w", err)
	}
	run, err := scoringRunFromRow(row)
	if err != nil {
		return ScoringRunSummary{}, err
	}
	return s.summarize(ctx, run)
}

// ScoringRuns returns the limit most recent runs, newest first, each with
// its scored and pending counts.
func (s *Store) ScoringRuns(ctx context.Context, limit int) ([]ScoringRunSummary, error) {
	rows, err := s.queries.ListScoringRuns(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("store: scoring runs: %w", err)
	}
	runs := make([]ScoringRunSummary, len(rows))
	for i, row := range rows {
		run, err := scoringRunFromRow(row)
		if err != nil {
			return nil, err
		}
		if runs[i], err = s.summarize(ctx, run); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

func (s *Store) summarize(ctx context.Context, run ScoringRun) (ScoringRunSummary, error) {
	scored, err := s.queries.CountScoresInRun(ctx, run.ID)
	if err != nil {
		return ScoringRunSummary{}, fmt.Errorf("store: scoring run %d: %w", run.ID, err)
	}
	pending, err := s.PendingPostings(ctx, run.ID, 0, 1)
	if err != nil {
		return ScoringRunSummary{}, err
	}
	return ScoringRunSummary{ScoringRun: run, Scored: int(scored), Pending: pending.Total}, nil
}

// scoringRun is the run with id, or ErrNotFound: foreign keys aren't
// enforced (00006), so nothing else stops a score naming a run that
// doesn't exist.
func (s *Store) scoringRun(ctx context.Context, id int64) (ScoringRun, error) {
	row, err := s.queries.GetScoringRun(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ScoringRun{}, fmt.Errorf("store: scoring run %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return ScoringRun{}, fmt.Errorf("store: scoring run %d: %w", id, err)
	}
	return scoringRunFromRow(row)
}

// PendingReason is why a posting is pending in a run.
type PendingReason int

const (
	// PendingNew: the posting has never been scored.
	PendingNew PendingReason = iota
	// PendingChanged: it was scored, but its content has changed since.
	PendingChanged
	// PendingRescore: it has a score on its current content, and the run
	// rescores everything.
	PendingRescore
)

// pendingReasonNames is the single name table for PendingReason, indexed
// by its value.
var pendingReasonNames = [...]string{
	PendingNew:     "new",
	PendingChanged: "changed",
	PendingRescore: "rescore",
}

func (r PendingReason) String() string {
	if r < 0 || int(r) >= len(pendingReasonNames) {
		return fmt.Sprintf("PendingReason(%d)", int(r))
	}
	return pendingReasonNames[r]
}

// PendingPosting is a posting a run still has to score: enough to judge
// it from, and the content hash its score must name.
type PendingPosting struct {
	ID            int64
	Title         string
	CompanyName   string
	Department    string
	Location      string
	WorkplaceType string
	// PublishedAt is zero when the board didn't say.
	PublishedAt   time.Time
	ContentSHA256 string
	Reason        PendingReason
}

// PendingPostingsPage is one page of a run's pending postings.
type PendingPostingsPage struct {
	Postings []PendingPosting
	// HasMore is whether pending postings follow this page.
	HasMore bool
	// Total is how many are pending in all, when the page was read; 0 on
	// an empty page.
	Total int
}

// PendingPostings returns one page of runID's pending postings, by ID,
// after afterID (0 for the first page). Eligible postings are open, not
// archived, not interested, without an application, at a company the
// user hasn't deleted, and let through by its filters. One is pending
// when it has no score in this run, and either the run rescores
// everything or it has no score on its current content.
func (s *Store) PendingPostings(ctx context.Context, runID, afterID int64, limit int) (PendingPostingsPage, error) {
	if limit <= 0 {
		return PendingPostingsPage{}, fmt.Errorf("store: pending postings: limit %d, want at least 1", limit)
	}
	run, err := s.scoringRun(ctx, runID)
	if err != nil {
		return PendingPostingsPage{}, err
	}
	params := db.ListPendingPostingsParams{
		RunID:      runID,
		RescoreAll: run.Scope == ScoringScopeAll,
		// One more than asked for, to learn whether another page follows.
		MaxRows: int64(limit) + 1,
	}
	if afterID != 0 {
		params.AfterID = afterID
	}
	rows, err := s.queries.ListPendingPostings(ctx, params)
	if err != nil {
		return PendingPostingsPage{}, fmt.Errorf("store: pending postings: %w", err)
	}
	page := PendingPostingsPage{}
	if len(rows) > limit {
		page.HasMore = true
		rows = rows[:limit]
	}
	page.Postings = make([]PendingPosting, len(rows))
	for i, row := range rows {
		page.Total = int(row.Total)
		reason := PendingNew
		switch {
		case row.HasCurrentScore:
			reason = PendingRescore
		case row.HasScore:
			reason = PendingChanged
		}
		page.Postings[i] = PendingPosting{
			ID:            row.ID,
			Title:         row.Title,
			CompanyName:   row.CompanyName,
			Department:    row.Department,
			Location:      row.Location,
			WorkplaceType: row.WorkplaceType,
			PublishedAt:   row.PublishedAt.Time,
			ContentSHA256: row.ContentSha256,
			Reason:        reason,
		}
	}
	return page, nil
}

// Scores are 1 to 5: coarse enough that a 4
// means the same from run to run and model to model, where 0-100 would
// ask the agent for precision it would invent.
const (
	minScore = 1
	maxScore = 5
)

// ScoreEntry is one posting's score, as the agent gives it.
type ScoreEntry struct {
	PostingID int64
	// Score is 1 to 5.
	Score int
	// Reason is the one line shown with the score.
	Reason string
	// ContentSHA256 is the posting's content hash the agent read, which
	// must still be current.
	ContentSHA256 string
}

// ScoreRefusal is why RecordScores didn't record an entry.
type ScoreRefusal int

const (
	// ScoreNoSuchPosting: no posting has the entry's ID.
	ScoreNoSuchPosting ScoreRefusal = iota
	// ScoreOutOfRange: the score isn't between 1 and 5.
	ScoreOutOfRange
	// ScoreNoReason: the reason is empty.
	ScoreNoReason
	// ScoreContentChanged: the posting's content changed since the agent
	// read it; it stays pending.
	ScoreContentChanged
	// ScoreAlreadyInRun: the posting already has a score in this run.
	ScoreAlreadyInRun
)

// scoreRefusalNames is the single name table for ScoreRefusal, indexed by
// its value.
var scoreRefusalNames = [...]string{
	ScoreNoSuchPosting:  "no_such_posting",
	ScoreOutOfRange:     "score_out_of_range",
	ScoreNoReason:       "no_reason",
	ScoreContentChanged: "content_changed",
	ScoreAlreadyInRun:   "already_scored_in_run",
}

func (r ScoreRefusal) String() string {
	if r < 0 || int(r) >= len(scoreRefusalNames) {
		return fmt.Sprintf("ScoreRefusal(%d)", int(r))
	}
	return scoreRefusalNames[r]
}

// RefusedScore is an entry RecordScores didn't record, and why.
type RefusedScore struct {
	PostingID int64
	Why       ScoreRefusal
}

// RecordScoresResult is what RecordScores did with each entry.
type RecordScoresResult struct {
	// Recorded is the posting IDs scored, in the entries' order.
	Recorded []int64
	// Refused is the entries not recorded, in the entries' order.
	Refused []RefusedScore
}

// RecordScores records entries as scores in runID, in one transaction:
// every valid entry is committed together, or none is if the write fails.
// An entry that can't be recorded is refused, and the result says why;
// an unknown run is ErrNotFound, with nothing recorded.
func (s *Store) RecordScores(ctx context.Context, runID int64, entries []ScoreEntry) (RecordScoresResult, error) {
	if _, err := s.scoringRun(ctx, runID); err != nil {
		return RecordScoresResult{}, err
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return RecordScoresResult{}, fmt.Errorf("store: begin record scores tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.queries.WithTx(tx)

	var result RecordScoresResult
	for _, e := range entries {
		why, ok, err := checkScoreEntry(ctx, qtx, runID, e)
		if err != nil {
			return RecordScoresResult{}, err
		}
		if !ok {
			result.Refused = append(result.Refused, RefusedScore{PostingID: e.PostingID, Why: why})
			continue
		}
		if err := qtx.CreatePostingScore(ctx, db.CreatePostingScoreParams{
			RunID:         runID,
			PostingID:     e.PostingID,
			Score:         int64(e.Score),
			Reason:        e.Reason,
			ContentSha256: e.ContentSHA256,
		}); err != nil {
			return RecordScoresResult{}, fmt.Errorf("store: record score for posting %d: %w", e.PostingID, err)
		}
		result.Recorded = append(result.Recorded, e.PostingID)
	}
	if err := tx.Commit(); err != nil {
		return RecordScoresResult{}, fmt.Errorf("store: commit record scores tx: %w", err)
	}
	return result, nil
}

// checkScoreEntry says whether e can be recorded in runID, and if not, why.
// It reads inside the transaction, so an entry earlier in the same batch
// counts as already scored.
func checkScoreEntry(ctx context.Context, qtx *db.Queries, runID int64, e ScoreEntry) (ScoreRefusal, bool, error) {
	switch {
	case e.Score < minScore || e.Score > maxScore:
		return ScoreOutOfRange, false, nil
	case e.Reason == "":
		return ScoreNoReason, false, nil
	}
	current, err := qtx.GetPostingContentSHA256(ctx, e.PostingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ScoreNoSuchPosting, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("store: posting %d: %w", e.PostingID, err)
	case current != e.ContentSHA256:
		return ScoreContentChanged, false, nil
	}
	scored, err := qtx.HasScoreInRun(ctx, db.HasScoreInRunParams{RunID: runID, PostingID: e.PostingID})
	if err != nil {
		return 0, false, fmt.Errorf("store: posting %d: %w", e.PostingID, err)
	}
	if scored {
		return ScoreAlreadyInRun, false, nil
	}
	return 0, true, nil
}
