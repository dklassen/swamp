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

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// Candidate is one posting ready for an external agent to work on: full
// posting content plus, if an application already exists for it, its id
// and status.
type Candidate struct {
	Posting           store.Posting
	CompanyName       string
	ApplicationID     *int64
	ApplicationStatus *store.ApplicationStatus
}

// Document is one document's resolved path and whether it already exists
// on disk, so an agent can tell a partially-generated application apart
// from a fresh one.
type Document struct {
	Path   string
	Exists bool
}

// Prepared is everything an external agent needs to draft and write one
// posting's cover letter and resume, once Prepare has committed to it.
type Prepared struct {
	Posting       store.Posting
	CompanyName   string
	ApplicationID int64
	CoverLetter   Document
	Resume        Document
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
		if p.ApplicationID != nil {
			status := st.documents.Status(*p.ApplicationID)
			if status.CoverLetter.Exists && status.Resume.Exists {
				continue
			}
		}
		candidates = append(candidates, Candidate{
			Posting:           p.Posting,
			CompanyName:       p.CompanyName,
			ApplicationID:     p.ApplicationID,
			ApplicationStatus: p.ApplicationStatus,
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

	return &Prepared{
		Posting:       posting,
		CompanyName:   company.Name,
		ApplicationID: application.ID,
		CoverLetter:   Document{Path: status.CoverLetter.Path, Exists: status.CoverLetter.Exists},
		Resume:        Document{Path: status.Resume.Path, Exists: status.Resume.Exists},
	}, nil
}
