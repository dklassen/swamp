package greenhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/jobboard"
)

// formDocuments maps the field names Greenhouse uses for its document
// uploads to Swamp's document types.
var formDocuments = map[string]documents.Type{
	"resume":       documents.Resume,
	"cover_letter": documents.CoverLetter,
}

// customQuestionPrefix starts the field name of every question the
// employer added to the form. The rest of "questions" are Greenhouse's
// standard fields (name, email, phone, the document uploads).
const customQuestionPrefix = "question_"

// fieldTypes maps Greenhouse's field types to jobboard.Question's. A
// hidden field isn't shown to the applicant, so it isn't a question.
var fieldTypes = map[string]string{
	"input_text":                "text",
	"textarea":                  "long_text",
	"multi_value_single_select": "single_select",
	"multi_value_multi_select":  "multi_select",
	"input_file":                "file",
}

type formResponse struct {
	// Only "questions" is read. location_questions, demographic_questions
	// and compliance are separate keys, so they're left out by not
	// reading them: nothing should draft those.
	Questions []struct {
		Label    string `json:"label"`
		Required bool   `json:"required"`
		Fields   []struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Values []struct {
				Label string `json:"label"`
			} `json:"values"`
		} `json:"fields"`
	} `json:"questions"`
}

// FetchApplicationForm fetches what jobID's application form asks for,
// from the job's ?questions=true detail (the list request Swamp syncs
// with doesn't include it).
func (c *Client) FetchApplicationForm(ctx context.Context, boardToken, jobID string) (jobboard.ApplicationForm, error) {
	url := fmt.Sprintf("%s/v1/boards/%s/jobs/%s?questions=true", c.baseURL, boardToken, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return jobboard.ApplicationForm{}, fmt.Errorf("greenhouse: build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return jobboard.ApplicationForm{}, fmt.Errorf("greenhouse: fetch application form: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return jobboard.ApplicationForm{}, fmt.Errorf("greenhouse: unexpected status %d fetching application form for job %s on %q", resp.StatusCode, jobID, boardToken)
	}

	var body formResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return jobboard.ApplicationForm{}, fmt.Errorf("greenhouse: decode application form: %w", err)
	}

	form := jobboard.ApplicationForm{Documents: map[documents.Type]jobboard.Requirement{}}
	for _, q := range body.Questions {
		if len(q.Fields) == 0 {
			continue
		}
		field := q.Fields[0]
		if documentType, ok := formDocuments[field.Name]; ok {
			requirement := jobboard.Optional
			if q.Required {
				requirement = jobboard.Required
			}
			form.Documents[documentType] = requirement
			continue
		}
		if !strings.HasPrefix(field.Name, customQuestionPrefix) || field.Type == "input_hidden" {
			continue
		}
		fieldType, ok := fieldTypes[field.Type]
		if !ok {
			fieldType = field.Type
		}
		question := jobboard.Question{Label: q.Label, Required: q.Required, Type: fieldType}
		for _, v := range field.Values {
			question.Options = append(question.Options, v.Label)
		}
		form.Questions = append(form.Questions, question)
	}
	return form, nil
}
