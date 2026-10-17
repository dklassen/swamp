// Package greenhouse is a client for Greenhouse's public Job Board API
// (https://docs.greenhouse.io/job-board.html). It fetches published
// postings for a company's Greenhouse job board and normalizes them into
// the domain-shaped jobboard.Posting type, preserving each posting's raw
// JSON so nothing is lost even for fields not yet mapped.
//
// Greenhouse's job shape differs from Ashby's in ways that don't map
// cleanly: a job can belong to multiple departments (joined here into one
// semicolon-separated string, since store/filter expect a single value
// per posting), there's no separate application URL (JobURL and
// ApplicationURL are the same absolute_url), there's no employment/
// workplace type field at all (Team, EmploymentType, WorkplaceType are
// simply left at their zero value -- see jobboard's doc comment), and
// there's no plain-text description -- only HTML, stripped here on a
// best-effort basis (see html.go) since DescriptionText is what an
// external agent drafts a cover letter from (see the stage package).
package greenhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dklassen/swamp/jobboard"
)

const defaultBaseURL = "https://boards-api.greenhouse.io"

type Client struct {
	httpClient *http.Client
	baseURL    string
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.httpClient = httpClient }
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: http.DefaultClient,
		baseURL:    defaultBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type jobBoardResponse struct {
	Jobs []json.RawMessage `json:"jobs"`
}

type department struct {
	Name string `json:"name"`
}

type location struct {
	Name string `json:"name"`
}

type job struct {
	ID          int64        `json:"id"`
	Title       string       `json:"title"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Location    location     `json:"location"`
	AbsoluteURL string       `json:"absolute_url"`
	Content     string       `json:"content"`
	Departments []department `json:"departments"`
}

// FetchPostings fetches boardToken's published postings. Always requests
// ?content=true -- without it, Greenhouse omits departments and the
// description entirely, and department is required for this app's
// filtering to work at all.
func (c *Client) FetchPostings(ctx context.Context, boardToken string) ([]jobboard.Posting, error) {
	url := fmt.Sprintf("%s/v1/boards/%s/jobs?content=true", c.baseURL, boardToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("greenhouse: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("greenhouse: fetch job board: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("greenhouse: unexpected status %d fetching job board %q", resp.StatusCode, boardToken)
	}

	var body jobBoardResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("greenhouse: decode job board response: %w", err)
	}

	postings := make([]jobboard.Posting, len(body.Jobs))
	for i, raw := range body.Jobs {
		var j job
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("greenhouse: decode job posting: %w", err)
		}
		// Greenhouse's content field is HTML-entity-double-encoded: its
		// JSON string value is itself entity-escaped markup (e.g.
		// "&lt;p&gt;" rather than "<p>"), confirmed against a real board's
		// live response -- decode once to recover actual HTML before
		// storing it or stripping tags from it.
		descriptionHTML := html.UnescapeString(j.Content)
		postings[i] = jobboard.Posting{
			SourceID:        strconv.FormatInt(j.ID, 10),
			Title:           j.Title,
			Department:      joinDepartments(j.Departments),
			Location:        j.Location.Name,
			DescriptionHTML: descriptionHTML,
			DescriptionText: stripHTML(descriptionHTML),
			JobURL:          j.AbsoluteURL,
			ApplicationURL:  j.AbsoluteURL,
			PublishedAt:     j.UpdatedAt,
			RawPayload:      []byte(raw),
		}
	}
	return postings, nil
}

func joinDepartments(departments []department) string {
	names := make([]string, len(departments))
	for i, d := range departments {
		names[i] = d.Name
	}
	return strings.Join(names, "; ")
}
