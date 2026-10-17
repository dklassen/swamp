// Package posting is a job posting as ingested from its company's board.
package posting

import (
	"time"

	"github.com/dklassen/swamp/contenthash"
)

// IngestedFields is a posting's content as ingested from its source --
// exactly the fields a re-fetch needs to compare to detect a real
// content change. Pulled into one type so there's a single place
// defining "what counts as a posting's content," rather than that field
// list being hand-copied at every site that needs it (#57).
//
// json tags pin the field names exactly as they already serialize today
// (Go's default reflect-based names) rather than changing them -- the
// point is making a future Go-side rename require an explicit tag edit
// to also change the JSON contract an external LLM agent reads
// (.agents/skills/apply-to-posting/SKILL.md), not changing that contract
// now (#59).
type IngestedFields struct {
	Title           string       `json:"Title"`
	Department      string       `json:"Department"`
	Team            string       `json:"Team"`
	Location        string       `json:"Location"`
	EmploymentType  string       `json:"EmploymentType"`
	WorkplaceType   string       `json:"WorkplaceType"`
	DescriptionHTML string       `json:"DescriptionHTML"`
	DescriptionText string       `json:"DescriptionText"`
	JobURL          string       `json:"JobURL"`
	ApplicationURL  string       `json:"ApplicationURL"`
	PublishedAt     OptionalTime `json:"PublishedAt"`
	RawPayload      string       `json:"RawPayload"`
}

// Posting is a source-agnostic job posting: canonical fields are
// normalized across boards, RawPayload preserves the original response for
// anything not (yet) promoted to a canonical field.
//
// IngestedFields is deliberately left without its own json tag: an
// anonymous field with no tag stays promoted (its fields serialize
// directly into this struct's own JSON object, matching how the current
// agent hand-off JSON already looks), where a tag would instead nest it
// under an "IngestedFields" key and silently break that contract (see
// #59).
type Posting struct {
	ID        int64  `json:"ID"`
	CompanyID int64  `json:"CompanyID"`
	Source    string `json:"Source"`
	SourceID  string `json:"SourceID"`
	IngestedFields
	// ContentSHA256 is ContentSHA256 of the content: the version a fit
	// score is tied to.
	ContentSHA256 string    `json:"ContentSHA256"`
	ListingStatus string    `json:"ListingStatus"`
	FirstSeenAt   time.Time `json:"FirstSeenAt"`
	LastSeenAt    time.Time `json:"LastSeenAt"`
	CreatedAt     time.Time `json:"CreatedAt"`
	UpdatedAt     time.Time `json:"UpdatedAt"`
}

// ContentSHA256 is contenthash.Of the fields the agent reads.
func ContentSHA256(f IngestedFields) string {
	return contenthash.Of(contenthash.Posting{Title: f.Title, Department: f.Department, Location: f.Location, WorkplaceType: f.WorkplaceType, DescriptionText: f.DescriptionText})
}
