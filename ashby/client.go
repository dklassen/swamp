// Package ashby is a client for Ashby's public job board API
// (https://developers.ashbyhq.com/docs/public-job-posting-api). It fetches
// published postings for a company's Ashby job board and normalizes them
// into the domain-shaped Posting type, preserving each posting's raw JSON
// so nothing is lost even for fields not yet mapped.
package ashby

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.ashbyhq.com"

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

type jobBoardResponse struct {
	Jobs []json.RawMessage `json:"jobs"`
}

type job struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Department       string    `json:"department"`
	Team             string    `json:"team"`
	EmploymentType   string    `json:"employmentType"`
	Location         string    `json:"location"`
	WorkplaceType    string    `json:"workplaceType"`
	PublishedAt      time.Time `json:"publishedAt"`
	JobURL           string    `json:"jobUrl"`
	ApplyURL         string    `json:"applyUrl"`
	DescriptionHTML  string    `json:"descriptionHtml"`
	DescriptionPlain string    `json:"descriptionPlain"`
}

func (c *Client) FetchPostings(ctx context.Context, boardSlug string) ([]Posting, error) {
	url := fmt.Sprintf("%s/posting-api/job-board/%s?listedOnly=true", c.baseURL, boardSlug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("ashby: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ashby: fetch job board: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ashby: unexpected status %d fetching job board %q", resp.StatusCode, boardSlug)
	}

	var body jobBoardResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ashby: decode job board response: %w", err)
	}

	postings := make([]Posting, len(body.Jobs))
	for i, raw := range body.Jobs {
		var j job
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("ashby: decode job posting: %w", err)
		}
		postings[i] = Posting{
			SourceID:        j.ID,
			Title:           j.Title,
			Department:      j.Department,
			Team:            j.Team,
			Location:        j.Location,
			EmploymentType:  j.EmploymentType,
			WorkplaceType:   j.WorkplaceType,
			DescriptionHTML: j.DescriptionHTML,
			DescriptionText: j.DescriptionPlain,
			JobURL:          j.JobURL,
			ApplicationURL:  j.ApplyURL,
			PublishedAt:     j.PublishedAt,
			RawPayload:      []byte(raw),
		}
	}
	return postings, nil
}
