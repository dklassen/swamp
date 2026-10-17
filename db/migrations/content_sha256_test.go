package migrations

import (
	"testing"

	"github.com/dklassen/swamp/contenthash"
)

// TestPostingContentSHA256_BackfillsExistingPostings: postings stored
// before 00026 get the hash IngestPosting would give them.
func TestPostingContentSHA256_BackfillsExistingPostings(t *testing.T) {
	t.Parallel()

	sqlDB := migrateTo(t, 26)
	if _, err := sqlDB.Exec(`INSERT INTO companies (name, source, source_ref) VALUES ('Acme', 'ashby', 'acme')`); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	postings := []contenthash.Posting{
		{Title: "Engineer", Department: "Platform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."},
		{Title: "Designer"},
	}
	for i, p := range postings {
		if _, err := sqlDB.Exec(`INSERT INTO postings (company_id, source, source_id, title, department, location, workplace_type, description_text, job_url, raw_payload)
			VALUES (1, 'ashby', ?, ?, ?, ?, ?, ?, 'https://jobs.example.com', '{}')`,
			i, p.Title, p.Department, p.Location, p.WorkplaceType, p.DescriptionText); err != nil {
			t.Fatalf("insert posting: %v", err)
		}
	}

	if err := upTo(sqlDB, 27); err != nil {
		t.Fatalf("migrate to 27: %v", err)
	}

	for i, p := range postings {
		var got string
		if err := sqlDB.QueryRow(`SELECT content_sha256 FROM postings WHERE source_id = ?`, i).Scan(&got); err != nil {
			t.Fatalf("read hash: %v", err)
		}
		if want := contenthash.Of(p); got != want {
			t.Errorf("posting %q: content_sha256 = %q, want %q", p.Title, got, want)
		}
	}
}
