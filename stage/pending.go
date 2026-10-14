package stage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/dklassen/swamp/cursor"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// pendingSort names list_pending_postings' one order in its cursors, so a
// search_postings cursor isn't taken for one.
const pendingSort = "pending"

// PendingOptions is list_pending_postings' input.
type PendingOptions struct {
	// ProfileSHA256 is read_profile's, which must still be profile.md's.
	ProfileSHA256 string
	// Rescore lists postings with no score against this profile, rather
	// than none at all, on their current content.
	Rescore bool
	// Cursor is the previous page's NextCursor, passed back unchanged;
	// empty starts from the beginning.
	Cursor string
	// Limit is the page size: defaultSearchLimit when 0 or less, never
	// more than maxSearchLimit.
	Limit int
}

// PendingResult is one page of pending postings.
type PendingResult struct {
	Postings []PendingMatch `json:"Postings"`
	// NextCursor is what to pass as Cursor for the next page; nil on the
	// last page.
	NextCursor *string `json:"NextCursor"`
	// Total is how many are pending in all, when this page was read.
	Total int `json:"Total"`
}

// PendingMatch is a posting still to score: enough to judge it from, the
// content hash its score must name, and why it's pending.
type PendingMatch struct {
	ID            int64               `json:"ID"`
	Title         string              `json:"Title"`
	CompanyName   string              `json:"CompanyName"`
	Department    string              `json:"Department"`
	Location      string              `json:"Location"`
	WorkplaceType string              `json:"WorkplaceType"`
	PublishedAt   store.OptionalTime  `json:"PublishedAt"`
	ContentSHA256 string              `json:"ContentSHA256"`
	Reason        store.PendingReason `json:"Reason"`
}

// pendingFilters is what a list_pending_postings cursor is bound to.
type pendingFilters struct {
	Rescore       bool
	ProfileSHA256 string
}

// PendingPostings returns one page of the postings still to score.
func (st *Stage) PendingPostings(ctx context.Context, opts PendingOptions) (PendingResult, error) {
	if err := st.checkProfile(opts.ProfileSHA256); err != nil {
		return PendingResult{}, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	limit = min(limit, maxSearchLimit)

	filters := pendingFilters{Rescore: opts.Rescore}
	var rescoreProfile string
	if opts.Rescore {
		filters.ProfileSHA256, rescoreProfile = opts.ProfileSHA256, opts.ProfileSHA256
	}
	fingerprint := cursor.Fingerprint(filters)
	var beforeID int64
	if opts.Cursor != "" {
		key, err := cursor.Decode(opts.Cursor, pendingSort, fingerprint)
		if err != nil {
			return PendingResult{}, fmt.Errorf("stage: %w", err)
		}
		beforeID = key.ID
	}

	page, err := st.store.PendingPostings(ctx, rescoreProfile, beforeID, limit)
	if err != nil {
		return PendingResult{}, fmt.Errorf("stage: %w", err)
	}
	result := PendingResult{Postings: make([]PendingMatch, len(page.Postings)), Total: page.Total}
	for i, p := range page.Postings {
		result.Postings[i] = PendingMatch{
			ID:            p.ID,
			Title:         p.Title,
			CompanyName:   p.CompanyName,
			Department:    p.Department,
			Location:      p.Location,
			WorkplaceType: p.WorkplaceType,
			PublishedAt:   store.OptionalTime{Time: p.PublishedAt},
			ContentSHA256: p.ContentSHA256,
			Reason:        p.Reason,
		}
	}
	if page.HasMore {
		next := cursor.Encode(cursor.Key{Sort: pendingSort, Filters: fingerprint, ID: page.Postings[len(page.Postings)-1].ID})
		result.NextCursor = &next
	}
	return result, nil
}

// checkProfile refuses a profile hash that isn't profile.md's now, so a
// score can't claim a profile the agent didn't read.
func (st *Stage) checkProfile(sha256 string) error {
	path := st.documents.ProfilePath()
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stage: there's no profile at %s to score against", path)
	}
	if err != nil {
		return fmt.Errorf("stage: read profile: %w", err)
	}
	if documents.ContentSHA256(string(content)) != sha256 {
		return errors.New("stage: the profile isn't the one whose SHA256 you gave (it changed, or wasn't read): call read_profile and pass its SHA256")
	}
	return nil
}
