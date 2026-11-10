// Package stage prepares interested postings for an external agent to
// draft a tailored cover letter and resume: List discovers postings ready
// for that hand-off, Prepare commits to one by ensuring its application
// and document directory exist. Stage never drafts content itself, and
// never reads the profile or canonical resume -- generation happens
// entirely outside this codebase.
package stage

import (
	"context"
	"fmt"
	"time"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/jobboard"
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
// (#59).
type Candidate struct {
	Posting           PostingSummary                 `json:"Posting"`
	CompanyName       string                         `json:"CompanyName"`
	ApplicationID     *int64                         `json:"ApplicationID"`
	ApplicationStatus *application.Status            `json:"ApplicationStatus"`
	ApplicationNotes  string                         `json:"ApplicationNotes"`
	LatestReviews     map[document.Type]LatestReview `json:"LatestReviews"`
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
	// PublishedAt is when the board says it was published; null when it
	// didn't say. Shown so newest-first search results say why (#224).
	PublishedAt posting.OptionalTime `json:"PublishedAt"`
	// FirstSeenAt is when Swamp first stored the posting.
	FirstSeenAt time.Time `json:"FirstSeenAt"`
}

func postingSummary(p posting.Posting) PostingSummary {
	return PostingSummary{
		ID:             p.ID,
		Title:          p.Title,
		Department:     p.Department,
		Location:       p.Location,
		WorkplaceType:  p.WorkplaceType,
		ApplicationURL: p.ApplicationURL,
		PublishedAt:    p.PublishedAt,
		FirstSeenAt:    p.FirstSeenAt,
	}
}

// PreparedPosting is the posting part of Prepared: everything needed to
// draft, without RawPayload and DescriptionHTML. Drafting reads
// DescriptionText, and those two were most of the response (#117).
type PreparedPosting struct {
	ID              int64                 `json:"ID"`
	CompanyID       int64                 `json:"CompanyID"`
	Source          string                `json:"Source"`
	SourceID        string                `json:"SourceID"`
	Title           string                `json:"Title"`
	Department      string                `json:"Department"`
	Team            string                `json:"Team"`
	Location        string                `json:"Location"`
	EmploymentType  string                `json:"EmploymentType"`
	WorkplaceType   string                `json:"WorkplaceType"`
	DescriptionText string                `json:"DescriptionText"`
	JobURL          string                `json:"JobURL"`
	ApplicationURL  string                `json:"ApplicationURL"`
	PublishedAt     posting.OptionalTime  `json:"PublishedAt"`
	ListingStatus   posting.ListingStatus `json:"ListingStatus"`
	FirstSeenAt     time.Time             `json:"FirstSeenAt"`
	LastSeenAt      time.Time             `json:"LastSeenAt"`
	CreatedAt       time.Time             `json:"CreatedAt"`
	UpdatedAt       time.Time             `json:"UpdatedAt"`
}

func preparedPosting(p posting.Posting) PreparedPosting {
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
// from a fresh one. Version is the version on disk (service.NoDocumentYet when it
// doesn't exist), which write_document takes back as ReplacesVersion.
type Document struct {
	Path    string `json:"Path"`
	Exists  bool   `json:"Exists"`
	Version string `json:"Version"`
}

// LatestReview is the parts of a document.Review an external agent
// needs to decide whether/how to revise a document: the last verdict,
// any notes on what to fix, how many review cycles it's already been
// through, and when that verdict was recorded. ContentSnapshot/
// ContentSHA256 are deliberately omitted -- the agent works from the
// document's current content on disk (see Prepared.Documents),
// not a historical snapshot.
type LatestReview struct {
	Outcome   document.Outcome `json:"Outcome"`
	Notes     string           `json:"Notes"`
	Cycle     int64            `json:"Cycle"`
	CreatedAt time.Time        `json:"CreatedAt"`
}

// latestReviewsForJSON converts a document.Review map (as returned
// by DocumentService.LatestReviews) into the leaner LatestReview shape
// this package exposes over JSON.
func latestReviewsForJSON(reviews map[document.Type]document.Review) map[document.Type]LatestReview {
	out := make(map[document.Type]LatestReview, len(reviews))
	for documentType, r := range reviews {
		out[documentType] = LatestReview{Outcome: r.Outcome, Notes: r.Notes, Cycle: r.Cycle, CreatedAt: r.CreatedAt}
	}
	return out
}

// Prepared is everything an external agent needs to draft and write one
// posting's documents, once Prepare has committed to it. Documents has
// one entry per document type, keyed by its name ("cover_letter",
// "resume"), so a new type reaches the agent without a new field (RFC
// 0004).
type Prepared struct {
	Posting          PreparedPosting                `json:"Posting"`
	CompanyName      string                         `json:"CompanyName"`
	ApplicationID    int64                          `json:"ApplicationID"`
	Documents        map[document.Type]Document     `json:"Documents"`
	ApplicationNotes string                         `json:"ApplicationNotes"`
	LatestReviews    map[document.Type]LatestReview `json:"LatestReviews"`
	// ApplicationForm is what the posting's form asks for (#168), or null
	// when it isn't known: the board has no supported way to read it
	// (Ashby, Lever: #167), or the fetch failed. Draft both documents then,
	// as before forms were read.
	ApplicationForm *jobboard.ApplicationForm `json:"ApplicationForm"`
	// ApplicationFormError says why the form couldn't be read, when a fetch
	// was tried and failed.
	ApplicationFormError string `json:"ApplicationFormError,omitempty"`
}

type companyService interface {
	ListActiveWithOpenPostings(ctx context.Context) ([]company.Stats, error)
}

type postingService interface {
	GetWithCompany(ctx context.Context, postingID int64) (*posting.Posting, *company.Company, error)
	Search(ctx context.Context, search posting.Search) (*posting.SearchPage, error)
}

type documentService interface {
	Write(ctx context.Context, applicationID int64, documentType document.Type, content, version string, source document.Source) (string, error)
	Read(ctx context.Context, applicationID int64, documentType document.Type) (path, content string, err error)
}

type draftingService interface {
	Queue(ctx context.Context) ([]service.QueuedPosting, error)
	Prepare(ctx context.Context, postingID int64) (*service.Prepared, error)
}

type scoringService interface {
	Pending(ctx context.Context, rescoreProfileSHA256 string, beforeID int64, limit int) (*score.PendingPostingsPage, error)
	Record(ctx context.Context, provenance score.Provenance, entries []score.Entry) (*score.RecordResult, error)
}

// Stage is the single entry point for the agent hand-off mechanism,
// mirroring how sync.Syncer composes ashby and store: List and Prepare
// are its whole exported API.
type Stage struct {
	companies companyService
	postings  postingService
	docs      documentService
	scores    scoringService
	drafting  draftingService
	documents *documents.Store
}

func New(companies companyService, postings postingService, docs documentService, scores scoringService, drafting draftingService, d *documents.Store) *Stage {
	return &Stage{
		companies: companies,
		postings:  postings,
		docs:      docs,
		scores:    scores,
		drafting:  drafting,
		documents: d,
	}
}

// List returns the postings an agent can draft for, as
// DraftingService.Queue orders them. Read-only: it never creates an
// application or touches the filesystem, so it's safe to call as often as
// needed to check on outstanding work.
func (st *Stage) List(ctx context.Context) ([]Candidate, error) {
	queue, err := st.drafting.Queue(ctx)
	if err != nil {
		return nil, fmt.Errorf("stage: %w", err)
	}
	candidates := make([]Candidate, len(queue))
	for i, q := range queue {
		candidates[i] = Candidate{
			Posting:       postingSummary(q.Posting),
			CompanyName:   q.CompanyName,
			LatestReviews: latestReviewsForJSON(q.LatestReviews),
		}
		if q.Application != nil {
			id, status := q.Application.ID, q.Application.Status
			candidates[i].ApplicationID, candidates[i].ApplicationStatus = &id, &status
			candidates[i].ApplicationNotes = q.Application.Notes
		}
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
	prepared, err := st.drafting.Prepare(ctx, postingID)
	if err != nil {
		return nil, fmt.Errorf("stage: %w", err)
	}
	preparedDocuments := make(map[document.Type]Document, len(prepared.Documents))
	for documentType, doc := range prepared.Documents {
		preparedDocuments[documentType] = Document{Path: doc.Path, Exists: doc.Exists, Version: doc.Version}
	}
	return &Prepared{
		Posting:              preparedPosting(prepared.Posting),
		CompanyName:          prepared.Company.Name,
		ApplicationID:        prepared.Application.ID,
		Documents:            preparedDocuments,
		ApplicationNotes:     prepared.Application.Notes,
		LatestReviews:        latestReviewsForJSON(prepared.LatestReviews),
		ApplicationForm:      prepared.Form.Form,
		ApplicationFormError: prepared.Form.FetchError,
	}, nil
}
