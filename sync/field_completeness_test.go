package sync

import (
	"reflect"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/store"
)

func fieldNames(t reflect.Type) []string {
	names := make([]string, t.NumField())
	for i := range names {
		names[i] = t.Field(i).Name
	}
	sort.Strings(names)
	return names
}

// TestIngestedFields_CoversEveryPostingField guards the single place
// #57's mapping now depends on: a field added to jobboard.Posting (a new
// piece of source-agnostic content) but not to store.IngestedFields
// would silently never reach storage, since toIngestedFields has no
// per-field code left to forget. SourceID is excluded -- it identifies
// the posting rather than being ingested content, and lives directly on
// CreatePostingParams instead of IngestedFields.
func TestIngestedFields_CoversEveryPostingField(t *testing.T) {
	want := fieldNames(reflect.TypeOf(Posting{}))
	for i, name := range want {
		if name == "SourceID" {
			want = append(want[:i], want[i+1:]...)
			break
		}
	}

	got := fieldNames(reflect.TypeOf(store.IngestedFields{}))

	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("store.IngestedFields fields != jobboard.Posting fields (-want +got):\n%s", diff)
	}
}
