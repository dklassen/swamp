package migrations

// LoggedTable is a table whose changes the change_events triggers record.
type LoggedTable struct {
	// Key is the column the triggers record as row_id.
	Key string
	// Columns are the ones the triggers record, before and after.
	Columns []string
	// Ignored are the table's other columns, deliberately not recorded.
	Ignored []string
}

// Logged and NotLogged together name every table: the tests fail on one
// that's in neither, on a logged table's column that's in neither list,
// and on a trigger that doesn't match its list.
var Logged = map[string]LoggedTable{
	"applications": {
		Key:     "id",
		Columns: []string{"status", "notes", "deleted_at"},
		Ignored: []string{"posting_id", "created_at", "updated_at"},
	},
}

// NotLogged maps each table whose changes aren't recorded to why.
var NotLogged = map[string]string{
	"change_events":    "the change log itself",
	"goose_db_version": "migration bookkeeping",
	// Their rows are deleted and their IDs reused, so an old event would
	// name a different row.
	"company_filters":  "IDs are reused after deletes",
	"interview_stages": "IDs are reused after deletes",

	"application_status_history": "not logged yet",
	"companies":                  "not logged yet",
	"document_exports":           "not logged yet",
	"document_reviews":           "not logged yet",
	"document_writes":            "not logged yet",
	"postings":                   "not logged yet",
	"posting_history":            "not logged yet",
	"posting_markup":             "not logged yet",
	"posting_application_forms":  "not logged yet",
	"posting_tags":               "not logged yet",
	"tags":                       "not logged yet",
}
