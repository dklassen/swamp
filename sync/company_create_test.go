package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/dklassen/swamp/jobboard"
)

func TestCreateCompany(t *testing.T) {
	t.Parallel()

	fetcher := &perBoardFetcher{
		postings:  map[string][]jobboard.Posting{"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")}},
		errBoards: map[string]error{"typo": errors.New("404 not found")},
	}

	tests := []struct {
		name        string
		source      string
		sourceRef   string
		wantErr     bool
		wantCreated bool
	}{
		{name: "board resolves", source: "ashby", sourceRef: "acme", wantCreated: true},
		{name: "board rejects ref", source: "ashby", sourceRef: "typo", wantErr: true},
		{name: "unsupported source", source: "workday", sourceRef: "acme", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := context.Background()
			syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

			company, err := syncer.CreateCompany(ctx, "Acme", tt.source, tt.sourceRef)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CreateCompany err = %v, wantErr %v", err, tt.wantErr)
			}

			companies, err := s.ListActiveCompanies(ctx)
			if err != nil {
				t.Fatalf("ListActiveCompanies: %v", err)
			}
			if gotCreated := len(companies) == 1; gotCreated != tt.wantCreated {
				t.Fatalf("companies = %+v, want created = %v", companies, tt.wantCreated)
			}
			if tt.wantCreated && (company.Source != tt.source || company.SourceRef != tt.sourceRef) {
				t.Fatalf("company = %+v, want Source=%s SourceRef=%s", company, tt.source, tt.sourceRef)
			}
		})
	}
}
