// Package stage prepares interested postings for an external agent to
// draft a tailored cover letter and resume: List discovers postings ready
// for that hand-off, Prepare commits to one by ensuring its application
// and document directory exist. Stage never drafts content itself, and
// never reads PROFILE_REFERENCE.md -- generation happens entirely outside
// this codebase (see decisions.log and README's "Further Notes").
package stage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// Candidate is one posting ready for an external agent to work on: full
// posting content plus, if an application already exists for it, its id
// and status.
//
// json tags pin the field names this type already serializes to today,
// since the real consumer of this JSON is an external LLM agent
// following .agents/skills/apply-to-posting/SKILL.md's documented
// examples, not Go code -- an ordinary Go-side rename would otherwise
// silently break that hand-off with no compiler or test catching it
// (see decisions.log, #59).
type Candidate struct {
	Posting           store.Posting                       `json:"Posting"`
	CompanyName       string                              `json:"CompanyName"`
	ApplicationID     *int64                              `json:"ApplicationID"`
	ApplicationStatus *store.ApplicationStatus            `json:"ApplicationStatus"`
	ApplicationNotes  string                              `json:"ApplicationNotes"`
	LatestReviews     map[store.DocumentType]LatestReview `json:"LatestReviews"`
}

// Document is one document's resolved path and whether it already exists
// on disk, so an agent can tell a partially-generated application apart
// from a fresh one.
type Document struct {
	Path   string `json:"Path"`
	Exists bool   `json:"Exists"`
}

// LatestReview is the parts of a store.DocumentReview an external agent
// needs to decide whether/how to revise a document: the last verdict,
// any notes on what to fix, how many review cycles it's already been
// through, and when that verdict was recorded. ContentSnapshot/
// ContentSHA256 are deliberately omitted -- the agent works from the
// document's current content on disk (see Prepared.CoverLetter/Resume),
// not a historical snapshot.
type LatestReview struct {
	Outcome   store.ReviewOutcome `json:"Outcome"`
	Notes     string              `json:"Notes"`
	Cycle     int64               `json:"Cycle"`
	CreatedAt time.Time           `json:"CreatedAt"`
}

// latestReviewsForJSON converts a store.DocumentReview map (as returned
// by store.LatestDocumentReviews) into the leaner LatestReview shape
// this package exposes over JSON.
func latestReviewsForJSON(reviews map[store.DocumentType]store.DocumentReview) map[store.DocumentType]LatestReview {
	out := make(map[store.DocumentType]LatestReview, len(reviews))
	for documentType, r := range reviews {
		out[documentType] = LatestReview{Outcome: r.Outcome, Notes: r.Notes, Cycle: r.Cycle, CreatedAt: r.CreatedAt}
	}
	return out
}

// needsRework reports whether any of reviews' latest outcomes is
// ReviewOutcomeFlagged -- a flagged document needs another drafting
// pass even once its file exists on disk, so List keeps surfacing it
// rather than treating "both files exist" as "done" (see decisions.log).
func needsRework(reviews map[store.DocumentType]store.DocumentReview) bool {
	for _, r := range reviews {
		if r.Outcome == store.ReviewOutcomeFlagged {
			return true
		}
	}
	return false
}

// Prepared is everything an external agent needs to draft and write one
// posting's cover letter and resume, once Prepare has committed to it.
type Prepared struct {
	Posting          store.Posting                       `json:"Posting"`
	CompanyName      string                              `json:"CompanyName"`
	ApplicationID    int64                               `json:"ApplicationID"`
	CoverLetter      Document                            `json:"CoverLetter"`
	Resume           Document                            `json:"Resume"`
	ApplicationNotes string                              `json:"ApplicationNotes"`
	LatestReviews    map[store.DocumentType]LatestReview `json:"LatestReviews"`
}

// Stage is the single entry point for the agent hand-off mechanism,
// mirroring how sync.Syncer composes ashby and store: List and Prepare
// are its whole exported API.
type Stage struct {
	store     *store.Store
	documents *documents.Store
}

func New(s *store.Store, d *documents.Store) *Stage {
	return &Stage{store: s, documents: d}
}

// List returns interested, non-archived postings that don't yet have both
// documents on disk. Read-only: it never creates an application or
// touches the filesystem, so it's safe to call as often as needed to
// check on outstanding work.
func (st *Stage) List(ctx context.Context) ([]Candidate, error) {
	postings, err := st.store.ListInterestedPostings(ctx)
	if err != nil {
		return nil, fmt.Errorf("stage: list interested postings: %w", err)
	}

	candidates := make([]Candidate, 0, len(postings))
	for _, p := range postings {
		var notes string
		var reviews map[store.DocumentType]store.DocumentReview
		if p.ApplicationID != nil {
			reviews, err = st.store.LatestDocumentReviews(ctx, *p.ApplicationID)
			if err != nil {
				return nil, fmt.Errorf("stage: latest document reviews: %w", err)
			}
			status := st.documents.Status(*p.ApplicationID)
			if status.CoverLetter.Exists && status.Resume.Exists && !needsRework(reviews) {
				continue
			}
			application, err := st.store.GetApplication(ctx, p.Posting.ID)
			if err != nil {
				return nil, fmt.Errorf("stage: get application: %w", err)
			}
			notes = application.Notes
		}
		candidates = append(candidates, Candidate{
			Posting:           p.Posting,
			CompanyName:       p.CompanyName,
			ApplicationID:     p.ApplicationID,
			ApplicationStatus: p.ApplicationStatus,
			ApplicationNotes:  notes,
			LatestReviews:     latestReviewsForJSON(reviews),
		})
	}
	return candidates, nil
}

// Prepare commits to drafting postingID's application: creates its
// Application if one doesn't exist yet, ensures its document directory
// exists, and returns everything needed to draft and write its
// documents. Idempotent -- calling Prepare more than once for the same
// posting reuses the existing application and directory rather than
// erroring or duplicating.
func (st *Stage) Prepare(ctx context.Context, postingID int64) (*Prepared, error) {
	posting, err := st.store.GetPosting(ctx, postingID)
	if err != nil {
		return nil, fmt.Errorf("stage: get posting: %w", err)
	}

	company, err := st.store.GetCompany(ctx, posting.CompanyID)
	if err != nil {
		return nil, fmt.Errorf("stage: get company: %w", err)
	}

	application, err := st.store.GetApplication(ctx, postingID)
	if errors.Is(err, store.ErrNotFound) {
		application, err = st.store.CreateApplication(ctx, postingID)
	}
	if err != nil {
		return nil, fmt.Errorf("stage: get or create application: %w", err)
	}

	if _, err := st.documents.EnsureDir(application.ID); err != nil {
		return nil, fmt.Errorf("stage: ensure document directory: %w", err)
	}
	status := st.documents.Status(application.ID)

	reviews, err := st.store.LatestDocumentReviews(ctx, application.ID)
	if err != nil {
		return nil, fmt.Errorf("stage: latest document reviews: %w", err)
	}

	return &Prepared{
		Posting:          posting,
		CompanyName:      company.Name,
		ApplicationID:    application.ID,
		CoverLetter:      Document{Path: status.CoverLetter.Path, Exists: status.CoverLetter.Exists},
		Resume:           Document{Path: status.Resume.Path, Exists: status.Resume.Exists},
		ApplicationNotes: application.Notes,
		LatestReviews:    latestReviewsForJSON(reviews),
	}, nil
}
