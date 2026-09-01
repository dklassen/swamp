// Package lever is a client for Lever's public Postings API
// (https://github.com/lever/postings-api). It fetches published postings
// for a company's Lever job board and normalizes them into the
// domain-shaped Posting type, preserving each posting's raw JSON so
// nothing is lost even for fields not yet mapped.
//
// Unlike Ashby/Greenhouse, Lever's response is a bare JSON array, not an
// object wrapping a "jobs" field. Lever also provides a plain-text
// description natively (descriptionPlain) -- no HTML-stripping needed,
// unlike Greenhouse. categories.department is omitted from the JSON
// entirely (not sent as "") when a posting has no department, confirmed
// against a real board's live response (Ro, jobs.lever.co/ro).
// createdAt is milliseconds since the Unix epoch, not seconds.
package lever

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.lever.co"

type Posting struct {
	SourceID        string
	Title           string
	Department      string
	Team            string
	Location        string
	EmploymentType  string
	WorkplaceType   string
	DescriptionHTML string
	DescriptionText string
	JobURL          string
	ApplicationURL  string
	PublishedAt     time.Time
	RawPayload      []byte
}

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

// categories holds the fields Lever groups under a posting's
// "categories" object -- commitment is the closest equivalent to
// Ashby/Greenhouse's "employment type" (e.g. "Full-time", "Contract").
type categories struct {
	Location   string `json:"location"`
	Commitment string `json:"commitment"`
	Team       string `json:"team"`
	Department string `json:"department"`
}

type posting struct {
	ID               string     `json:"id"`
	Text             string     `json:"text"`
	Categories       categories `json:"categories"`
	CreatedAt        int64      `json:"createdAt"`
	Description      string     `json:"description"`
	DescriptionPlain string     `json:"descriptionPlain"`
	WorkplaceType    string     `json:"workplaceType"`
	HostedURL        string     `json:"hostedUrl"`
	ApplyURL         string     `json:"applyUrl"`
}

// FetchPostings fetches site's published postings. mode=json selects the
// JSON response format (Lever also supports html/iframe modes for
// embedding, not relevant here).
func (c *Client) FetchPostings(ctx context.Context, site string) ([]Posting, error) {
	url := fmt.Sprintf("%s/v0/postings/%s?mode=json", c.baseURL, site)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("lever: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lever: fetch postings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lever: unexpected status %d fetching postings for site %q", resp.StatusCode, site)
	}

	var raw []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("lever: decode postings response: %w", err)
	}

	postings := make([]Posting, len(raw))
	for i, r := range raw {
		var p posting
		if err := json.Unmarshal(r, &p); err != nil {
			return nil, fmt.Errorf("lever: decode posting: %w", err)
		}
		postings[i] = Posting{
			SourceID:        p.ID,
			Title:           p.Text,
			Department:      p.Categories.Department,
			Team:            p.Categories.Team,
			Location:        p.Categories.Location,
			EmploymentType:  p.Categories.Commitment,
			WorkplaceType:   p.WorkplaceType,
			DescriptionHTML: p.Description,
			DescriptionText: p.DescriptionPlain,
			JobURL:          p.HostedURL,
			ApplicationURL:  p.ApplyURL,
			PublishedAt:     time.UnixMilli(p.CreatedAt).UTC(),
			RawPayload:      []byte(r),
		}
	}
	return postings, nil
}
