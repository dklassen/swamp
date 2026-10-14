package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// PendingReason is why a posting is pending.
type PendingReason int

const (
	// PendingNew: the posting has never been scored.
	PendingNew PendingReason = iota
	// PendingChanged: it was scored, but its content has changed since.
	PendingChanged
	// PendingRescore: it has a score on its current content, but not
	// against the profile a rescore is for.
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

// ParsePendingReason is the reason named s, or an error for any other name.
func ParsePendingReason(s string) (PendingReason, error) {
	for i, name := range pendingReasonNames {
		if name == s {
			return PendingReason(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown pending reason %q", s)
}

// MarshalText encodes r as its name, so JSON readers never see the const
// order.
func (r PendingReason) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

// UnmarshalText is MarshalText's inverse.
func (r *PendingReason) UnmarshalText(text []byte) error {
	parsed, err := ParsePendingReason(string(text))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// PendingPosting is a posting with no score yet on what it says now:
// enough to judge it from, and the content hash its score must name.
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

// PendingPostingsPage is one page of pending postings.
type PendingPostingsPage struct {
	Postings []PendingPosting
	// HasMore is whether pending postings follow this page.
	HasMore bool
	// Total is how many are pending in all, when the page was read; 0 on
	// an empty page.
	Total int
}

// PendingPostings returns one page of the postings with no score on their
// current content, by ID, after afterID (0 for the first page). For a
// rescore, rescoreProfileSHA256 names the profile, and a score against
// another one doesn't count; "" is no rescore. Eligible postings are open,
// not archived, not interested, without an application, at a company the
// user hasn't deleted, and let through by its filters.
func (s *Store) PendingPostings(ctx context.Context, rescoreProfileSHA256 string, afterID int64, limit int) (PendingPostingsPage, error) {
	if limit <= 0 {
		return PendingPostingsPage{}, fmt.Errorf("store: pending postings: limit %d, want at least 1", limit)
	}
	params := db.ListPendingPostingsParams{
		// One more than asked for, to learn whether another page follows.
		MaxRows: int64(limit) + 1,
	}
	if rescoreProfileSHA256 != "" {
		params.RescoreProfile = rescoreProfileSHA256
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

// Scores are 1 to 5: coarse enough that a 4 means the same from one
// scoring to the next and one model to another, where 0-100 would ask the
// agent for precision it would invent.
const (
	minScore = 1
	maxScore = 5
)

// ScoreProvenance is what produced a batch of scores. The client's name
// and version may be empty: MCP client info is optional.
type ScoreProvenance struct {
	Model         string
	ClientName    string
	ClientVersion string
	ProfileSHA256 string
}

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
	// ScoreRepeat: the posting already has a score on this content,
	// against this profile, from this model.
	ScoreRepeat
)

// scoreRefusalNames is the single name table for ScoreRefusal, indexed by
// its value.
var scoreRefusalNames = [...]string{
	ScoreNoSuchPosting:  "no_such_posting",
	ScoreOutOfRange:     "score_out_of_range",
	ScoreNoReason:       "no_reason",
	ScoreContentChanged: "content_changed",
	ScoreRepeat:         "already_scored",
}

func (r ScoreRefusal) String() string {
	if r < 0 || int(r) >= len(scoreRefusalNames) {
		return fmt.Sprintf("ScoreRefusal(%d)", int(r))
	}
	return scoreRefusalNames[r]
}

// ScoreRefusals returns every refusal, in const order.
func ScoreRefusals() []ScoreRefusal {
	refusals := make([]ScoreRefusal, len(scoreRefusalNames))
	for i := range scoreRefusalNames {
		refusals[i] = ScoreRefusal(i)
	}
	return refusals
}

// ParseScoreRefusal is the refusal named s, or an error for any other name.
func ParseScoreRefusal(s string) (ScoreRefusal, error) {
	for i, name := range scoreRefusalNames {
		if name == s {
			return ScoreRefusal(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown score refusal %q", s)
}

// MarshalText encodes r as its name, so JSON readers never see the const
// order.
func (r ScoreRefusal) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

// UnmarshalText is MarshalText's inverse.
func (r *ScoreRefusal) UnmarshalText(text []byte) error {
	parsed, err := ParseScoreRefusal(string(text))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
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

// RecordScores records entries as scores from provenance, in one
// transaction: every valid entry is committed together, or none is if the
// write fails. An entry that can't be recorded is refused, and the result
// says why. A call with no model or profile hash records nothing.
func (s *Store) RecordScores(ctx context.Context, provenance ScoreProvenance, entries []ScoreEntry) (RecordScoresResult, error) {
	switch {
	case provenance.Model == "":
		return RecordScoresResult{}, errors.New("store: record scores: model is required")
	case provenance.ProfileSHA256 == "":
		return RecordScoresResult{}, errors.New("store: record scores: profile hash is required")
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return RecordScoresResult{}, fmt.Errorf("store: begin record scores tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.queries.WithTx(tx)

	var result RecordScoresResult
	for _, e := range entries {
		why, ok, err := checkScoreEntry(ctx, qtx, provenance, e)
		if err != nil {
			return RecordScoresResult{}, err
		}
		if !ok {
			result.Refused = append(result.Refused, RefusedScore{PostingID: e.PostingID, Why: why})
			continue
		}
		if err := qtx.CreatePostingScore(ctx, db.CreatePostingScoreParams{
			PostingID:     e.PostingID,
			Score:         int64(e.Score),
			Reason:        e.Reason,
			ContentSha256: e.ContentSHA256,
			ProfileSha256: provenance.ProfileSHA256,
			Model:         provenance.Model,
			ClientName:    provenance.ClientName,
			ClientVersion: provenance.ClientVersion,
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

// checkScoreEntry says whether e can be recorded, and if not, why. It reads
// inside the transaction, so an entry earlier in the same batch counts.
func checkScoreEntry(ctx context.Context, qtx *db.Queries, provenance ScoreProvenance, e ScoreEntry) (ScoreRefusal, bool, error) {
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
	repeat, err := qtx.HasRepeatScore(ctx, db.HasRepeatScoreParams{
		PostingID:     e.PostingID,
		ContentSha256: e.ContentSHA256,
		ProfileSha256: provenance.ProfileSHA256,
		Model:         provenance.Model,
	})
	if err != nil {
		return 0, false, fmt.Errorf("store: posting %d: %w", e.PostingID, err)
	}
	if repeat {
		return ScoreRepeat, false, nil
	}
	return 0, true, nil
}

// PostingScore is one score a posting was given, and what produced it.
type PostingScore struct {
	ID int64
	ScoreProvenance
	Score         int
	Reason        string
	ContentSHA256 string
	ScoredAt      time.Time
}

// PostingScores returns every score postingID was given, newest, its
// current one, first.
func (s *Store) PostingScores(ctx context.Context, postingID int64) ([]PostingScore, error) {
	rows, err := s.queries.ListPostingScores(ctx, postingID)
	if err != nil {
		return nil, fmt.Errorf("store: scores of posting %d: %w", postingID, err)
	}
	scores := make([]PostingScore, len(rows))
	for i, row := range rows {
		scores[i] = PostingScore{
			ID: row.ID,
			ScoreProvenance: ScoreProvenance{
				Model:         row.Model,
				ClientName:    row.ClientName,
				ClientVersion: row.ClientVersion,
				ProfileSHA256: row.ProfileSha256,
			},
			Score:         int(row.Score),
			Reason:        row.Reason,
			ContentSHA256: row.ContentSha256,
			ScoredAt:      row.ScoredAt,
		}
	}
	return scores, nil
}
