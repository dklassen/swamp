package stage

import (
	"context"
	"fmt"

	"github.com/dklassen/swamp/scoring"
	"github.com/dklassen/swamp/store"
)

// RecordOptions is record_scores' input, and the client it was called
// from, which may be empty.
type RecordOptions struct {
	Model          string
	ProfileVersion string
	ClientName     string
	ClientVersion  string
	Scores         []ScoreInput
}

// ScoreInput is one posting's score, as the agent gives it.
type ScoreInput struct {
	PostingID      int64  `json:"PostingID"`
	Score          int    `json:"Score"`
	Reason         string `json:"Reason"`
	PostingVersion string `json:"PostingVersion"`
}

// RecordResult is what record_scores did with each score: both lists are
// in the order given, and empty rather than null.
type RecordResult struct {
	Recorded []int64        `json:"Recorded"`
	Refused  []RefusedEntry `json:"Refused"`
}

// RefusedEntry is a score that wasn't recorded, and why.
type RefusedEntry struct {
	PostingID int64           `json:"PostingID"`
	Why       scoring.Refusal `json:"Why"`
}

// RecordScores records the agent's scores against the profile it read, in
// one transaction. A stale profile hash records nothing.
func (st *Stage) RecordScores(ctx context.Context, opts RecordOptions) (RecordResult, error) {
	if err := st.checkProfile(opts.ProfileVersion); err != nil {
		return RecordResult{}, err
	}
	entries := make([]scoring.Entry, len(opts.Scores))
	for i, s := range opts.Scores {
		entries[i] = scoring.Entry{PostingID: s.PostingID, Score: s.Score, Reason: s.Reason, ContentSHA256: s.PostingVersion}
	}
	provenance := scoring.Provenance{
		Model:         opts.Model,
		ClientName:    opts.ClientName,
		ClientVersion: opts.ClientVersion,
		ProfileSHA256: opts.ProfileVersion,
	}
	var recorded scoring.RecordResult
	err := store.InTx(ctx, st.store.DB(), func(tx store.Tx) error {
		var err error
		recorded, err = scoring.Record(ctx, tx, provenance, entries)
		return err
	})
	if err != nil {
		return RecordResult{}, fmt.Errorf("stage: %w", err)
	}
	result := RecordResult{Recorded: append([]int64{}, recorded.Recorded...), Refused: make([]RefusedEntry, len(recorded.Refused))}
	for i, r := range recorded.Refused {
		result.Refused[i] = RefusedEntry{PostingID: r.PostingID, Why: r.Why}
	}
	return result, nil
}
