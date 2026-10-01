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
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// Candidate is one posting ready for an external agent to work on: a
// summary of the posting, enough to pick one (Prepare returns its content
// for drafting), plus, if an application already exists for it, its id
// and status.
//
// json tags pin the field names this type already serializes to today,
// since the real consumer of this JSON is an external LLM agent
// following .agents/skills/apply-to-posting/SKILL.md's documented
// examples, not Go code -- an ordinary Go-side rename would otherwise
// silently break that hand-off with no compiler or test catching it
// (see decisions.log, #59).
type Candidate struct {
	Posting           PostingSummary                  `json:"Posting"`
	CompanyName       string                          `json:"CompanyName"`
	ApplicationID     *int64                          `json:"ApplicationID"`
	ApplicationStatus *store.ApplicationStatus        `json:"ApplicationStatus"`
	ApplicationNotes  string                          `json:"ApplicationNotes"`
	LatestReviews     map[documents.Type]LatestReview `json:"LatestReviews"`
}

// PostingSummary is the posting part of a Candidate: enough for the user
// to pick a posting from the list. The description and raw payload are
// left out, since they made list_postings too large for agent clients:
// about 29 KB per posting, 950 KB for 33 (#117).
type PostingSummary struct {
	ID             int64  `json:"ID"`
	Title          string `json:"Title"`
	Department     string `json:"Department"`
	Location       string `json:"Location"`
	WorkplaceType  string `json:"WorkplaceType"`
	ApplicationURL string `json:"ApplicationURL"`
}

func postingSummary(p store.Posting) PostingSummary {
	return PostingSummary{
		ID:             p.ID,
		Title:          p.Title,
		Department:     p.Department,
		Location:       p.Location,
		WorkplaceType:  p.WorkplaceType,
		ApplicationURL: p.ApplicationURL,
	}
}

// PreparedPosting is the posting part of Prepared: everything needed to
// draft, without RawPayload and DescriptionHTML. Drafting reads
// DescriptionText, and those two were most of the response (#117).
type PreparedPosting struct {
	ID              int64              `json:"ID"`
	CompanyID       int64              `json:"CompanyID"`
	Source          string             `json:"Source"`
	SourceID        string             `json:"SourceID"`
	Title           string             `json:"Title"`
	Department      string             `json:"Department"`
	Team            string             `json:"Team"`
	Location        string             `json:"Location"`
	EmploymentType  string             `json:"EmploymentType"`
	WorkplaceType   string             `json:"WorkplaceType"`
	DescriptionText string             `json:"DescriptionText"`
	JobURL          string             `json:"JobURL"`
	ApplicationURL  string             `json:"ApplicationURL"`
	PublishedAt     store.OptionalTime `json:"PublishedAt"`
	ListingStatus   string             `json:"ListingStatus"`
	FirstSeenAt     time.Time          `json:"FirstSeenAt"`
	LastSeenAt      time.Time          `json:"LastSeenAt"`
	CreatedAt       time.Time          `json:"CreatedAt"`
	UpdatedAt       time.Time          `json:"UpdatedAt"`
}

func preparedPosting(p store.Posting) PreparedPosting {
	return PreparedPosting{
		ID:              p.ID,
		CompanyID:       p.CompanyID,
		Source:          p.Source,
		SourceID:        p.SourceID,
		Title:           p.Title,
		Department:      p.Department,
		Team:            p.Team,
		Location:        p.Location,
		EmploymentType:  p.EmploymentType,
		WorkplaceType:   p.WorkplaceType,
		DescriptionText: p.DescriptionText,
		JobURL:          p.JobURL,
		ApplicationURL:  p.ApplicationURL,
		PublishedAt:     p.PublishedAt,
		ListingStatus:   p.ListingStatus,
		FirstSeenAt:     p.FirstSeenAt,
		LastSeenAt:      p.LastSeenAt,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
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
// document's current content on disk (see Prepared.Documents),
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
func latestReviewsForJSON(reviews map[documents.Type]store.DocumentReview) map[documents.Type]LatestReview {
	out := make(map[documents.Type]LatestReview, len(reviews))
	for documentType, r := range reviews {
		out[documentType] = LatestReview{Outcome: r.Outcome, Notes: r.Notes, Cycle: r.Cycle, CreatedAt: r.CreatedAt}
	}
	return out
}

// needsRework reports whether any of reviews' latest outcomes is
// ReviewOutcomeFlagged -- a flagged document needs another drafting
// pass even once its file exists on disk, so List keeps surfacing it
// rather than treating "both files exist" as "done" (see decisions.log).
func needsRework(reviews map[documents.Type]store.DocumentReview) bool {
	for _, r := range reviews {
		if r.Outcome == store.ReviewOutcomeFlagged {
			return true
		}
	}
	return false
}

// Prepared is everything an external agent needs to draft and write one
// posting's documents, once Prepare has committed to it. Documents has
// one entry per document type, keyed by its name ("cover_letter",
// "resume"), so a new type reaches the agent without a new field (RFC
// 0004).
type Prepared struct {
	Posting          PreparedPosting                 `json:"Posting"`
	CompanyName      string                          `json:"CompanyName"`
	ApplicationID    int64                           `json:"ApplicationID"`
	Documents        map[documents.Type]Document     `json:"Documents"`
	ApplicationNotes string                          `json:"ApplicationNotes"`
	LatestReviews    map[documents.Type]LatestReview `json:"LatestReviews"`
	// ApplicationForm is what the posting's form asks for (#168), or null
	// when it isn't known: the board has no supported way to read it
	// (Ashby, Lever: #167), or the fetch failed. Draft both documents then,
	// as before forms were read.
	ApplicationForm *jobboard.ApplicationForm `json:"ApplicationForm"`
	// ApplicationFormError says why the form couldn't be read, when a fetch
	// was tried and failed.
	ApplicationFormError string `json:"ApplicationFormError,omitempty"`
}

// Stage is the single entry point for the agent hand-off mechanism,
// mirroring how sync.Syncer composes ashby and store: List and Prepare
// are its whole exported API.
type Stage struct {
	store     *store.Store
	documents *documents.Store
	// forms reads application forms, by company source (#168).
	forms        map[string]FormFetcher
	fetchTimeout time.Duration
	// now is the clock deciding whether a stored form is stale; tests
	// move it forward.
	now func() time.Time
}

// Option configures a Stage.
type Option func(*Stage)

func New(s *store.Store, d *documents.Store, opts ...Option) *Stage {
	st := &Stage{store: s, documents: d, now: time.Now}
	for _, opt := range opts {
		opt(st)
	}
	return st
}

// List returns the postings an agent can draft for: interested,
// non-archived postings, plus postings with a started application that
// were never marked interested (#165) -- an application started from the
// TUI's posting detail, say. Either way a posting is skipped once both
// documents are on disk and no current review is flagged, or when its
// application is at a terminal status (rejected, withdrawn, posting
// closed...). Started applications that aren't interested come after the
// interested postings. Read-only: it never creates an application or
// touches the filesystem, so it's safe to call as often as needed to
// check on outstanding work.
func (st *Stage) List(ctx context.Context) ([]Candidate, error) {
	postings, err := st.store.ListInterestedPostings(ctx)
	if err != nil {
		return nil, fmt.Errorf("stage: list interested postings: %w", err)
	}
	started, err := st.startedNotInterested(ctx, postings)
	if err != nil {
		return nil, err
	}

	candidates := make([]Candidate, 0, len(postings)+len(started))
	for _, p := range append(postings, started...) {
		c, ok, err := st.candidate(ctx, p)
		if err != nil {
			return nil, err
		}
		if ok {
			candidates = append(candidates, c)
		}
	}
	return candidates, nil
}

// startedNotInterested returns the postings with a started application
// that aren't among interested (so weren't marked interested) and aren't
// archived, in the shape ListInterestedPostings returns.
func (st *Stage) startedNotInterested(ctx context.Context, interested []store.InterestedPosting) ([]store.InterestedPosting, error) {
	seen := make(map[int64]bool, len(interested))
	for _, p := range interested {
		seen[p.Posting.ID] = true
	}
	applications, err := st.store.ListActiveApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("stage: list active applications: %w", err)
	}
	var started []store.InterestedPosting
	for _, a := range applications {
		if a.Status != store.ApplicationStatusStarted || seen[a.Posting.ID] {
			continue
		}
		markup, err := st.store.GetPostingMarkup(ctx, a.Posting.ID)
		if err != nil {
			return nil, fmt.Errorf("stage: get posting markup: %w", err)
		}
		if markup.ArchivedAt != nil {
			continue
		}
		id, status := a.ID, a.Status
		started = append(started, store.InterestedPosting{
			Posting:           a.Posting,
			CompanyName:       a.CompanyName,
			ApplicationID:     &id,
			ApplicationStatus: &status,
		})
	}
	return started, nil
}

// candidate builds p's Candidate, or reports false when there's nothing
// left to draft: both documents are on disk and no current review is
// flagged.
func (st *Stage) candidate(ctx context.Context, p store.InterestedPosting) (Candidate, bool, error) {
	var notes string
	var reviews map[documents.Type]store.DocumentReview
	if p.ApplicationID != nil {
		latest, err := st.store.LatestDocumentReviews(ctx, *p.ApplicationID)
		if err != nil {
			return Candidate{}, false, fmt.Errorf("stage: latest document reviews: %w", err)
		}
		status := st.documents.Status(*p.ApplicationID)
		reviews, err = documents.Current(status, latest, store.DocumentReview.IsCurrent)
		if err != nil {
			return Candidate{}, false, fmt.Errorf("stage: check review currency: %w", err)
		}
		if allDrafted(status) && !needsRework(reviews) {
			return Candidate{}, false, nil
		}
		application, err := st.store.GetApplication(ctx, p.Posting.ID)
		if err != nil {
			return Candidate{}, false, fmt.Errorf("stage: get application: %w", err)
		}
		notes = application.Notes
	}
	return Candidate{
		Posting:           postingSummary(p.Posting),
		CompanyName:       p.CompanyName,
		ApplicationID:     p.ApplicationID,
		ApplicationStatus: p.ApplicationStatus,
		ApplicationNotes:  notes,
		LatestReviews:     latestReviewsForJSON(reviews),
	}, true, nil
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

	form, formErr, err := st.applicationForm(ctx, posting, company)
	if err != nil {
		return nil, err
	}

	reviews, err := st.store.LatestDocumentReviews(ctx, application.ID)
	if err != nil {
		return nil, fmt.Errorf("stage: latest document reviews: %w", err)
	}
	reviews, err = documents.Current(status, reviews, store.DocumentReview.IsCurrent)
	if err != nil {
		return nil, fmt.Errorf("stage: check review currency: %w", err)
	}

	preparedDocuments := make(map[documents.Type]Document, len(documents.Types()))
	for _, documentType := range documents.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			return nil, fmt.Errorf("stage: %w", err)
		}
		preparedDocuments[documentType] = Document{Path: doc.Path, Exists: doc.Exists}
	}

	return &Prepared{
		Posting:              preparedPosting(posting),
		CompanyName:          company.Name,
		ApplicationID:        application.ID,
		Documents:            preparedDocuments,
		ApplicationNotes:     application.Notes,
		LatestReviews:        latestReviewsForJSON(reviews),
		ApplicationForm:      form,
		ApplicationFormError: formErr,
	}, nil
}

// allDrafted reports whether every document type in documents' list
// exists for the application. Until RFC 0002 phase 2 records which
// documents a posting's form asks for (#168, #169), every type is
// required (RFC 0004, decided 2026-10-01).
func allDrafted(status documents.Status) bool {
	for _, documentType := range documents.Types() {
		doc, err := status.Doc(documentType)
		if err != nil || !doc.Exists {
			return false
		}
	}
	return true
}
