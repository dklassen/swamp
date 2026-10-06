package migrations

// LoggedTable is a table whose changes the change_events triggers record.
type LoggedTable struct {
	// Key is the column the triggers record as row_id.
	Key string
	// Columns are the ones the triggers record, before and after.
	Columns []string
	// Ignored are the table's other columns, deliberately not recorded.
	Ignored []string
	// Ops are the operations recorded: "insert", "update", "delete".
	Ops []string
}

// Logged and NotLogged together name every table: the tests fail on one
// that's in neither, on a logged table's column that's in neither list,
// and on a trigger that doesn't match its list.
var Logged = map[string]LoggedTable{
	"applications": {
		Key:     "id",
		Columns: []string{"status", "notes", "deleted_at"},
		Ignored: []string{"posting_id", "created_at", "updated_at"},
		Ops:     allOps,
	},
	"companies": {
		Key:     "id",
		Columns: []string{"name", "description", "deleted_at"},
		Ignored: []string{"source", "source_ref", "created_at", "updated_at", "last_fetched_at", "sync_lease_token", "sync_lease_at"},
		Ops:     allOps,
	},
	// updated_at only changes with a posting's content, not when a sync
	// merely sees it again (last_seen_at), so it marks a description
	// change without copying the description.
	"postings": {
		Key:     "id",
		Columns: []string{"company_id", "listing_status", "title", "department", "team", "location", "workplace_type", "employment_type", "published_at", "updated_at"},
		Ignored: []string{"source", "source_id", "description_html", "description_text", "job_url", "application_url", "raw_payload", "first_seen_at", "last_seen_at", "created_at"},
		Ops:     allOps,
	},
	"document_reviews": {
		Key:     "id",
		Columns: []string{"application_id", "document_type", "outcome"},
		Ignored: []string{"cycle", "content_snapshot", "content_sha256", "notes", "created_at"},
		Ops:     []string{"insert"},
	},
	"document_exports": {
		Key:     "id",
		Columns: []string{"application_id", "document_type", "content_sha256"},
		Ignored: []string{"path", "exported_at"},
		Ops:     []string{"insert"},
	},
	"document_writes": {
		Key:     "id",
		Columns: []string{"application_id", "document_type", "source", "content_sha256"},
		Ignored: []string{"written_at"},
		Ops:     []string{"insert"},
	},
}

var allOps = []string{"insert", "update", "delete"}

// NotLogged maps each table whose changes aren't recorded to why.
var NotLogged = map[string]string{
	"change_events":    "the change log itself",
	"goose_db_version": "migration bookkeeping",
	// Their rows are deleted and their IDs reused, so an old event would
	// name a different row.
	"company_filters":  "IDs are reused after deletes",
	"interview_stages": "IDs are reused after deletes",

	"application_status_history": "duplicates applications' status events",
	"posting_history":            "duplicates postings' events",

	// Logged once something reads their changes and acts on them.
	"posting_markup":            "no reader acts on its changes yet",
	"posting_application_forms": "no reader acts on its changes yet",
	"posting_tags":              "no reader acts on its changes yet",
	"tags":                      "no reader acts on its changes yet",
}
