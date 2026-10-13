package contenthash

import "testing"

func TestOf_EveryFieldChangesTheHash(t *testing.T) {
	t.Parallel()

	base := Posting{Title: "Engineer", Department: "Platform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."}
	tests := []struct {
		name    string
		changed Posting
	}{
		{"title", Posting{Title: "Engineer II", Department: "Platform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."}},
		{"department", Posting{Title: "Engineer", Department: "Data", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."}},
		{"location", Posting{Title: "Engineer", Department: "Platform", Location: "Cork", WorkplaceType: "Hybrid", DescriptionText: "Build things."}},
		{"workplace type", Posting{Title: "Engineer", Department: "Platform", Location: "Dublin", WorkplaceType: "Remote", DescriptionText: "Build things."}},
		{"description", Posting{Title: "Engineer", Department: "Platform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build other things."}},
		// The same text split differently between fields is other content.
		{"a field boundary", Posting{Title: "EngineerP", Department: "latform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if Of(base) == Of(tt.changed) {
				t.Errorf("changing the %s left the hash at %s", tt.name, Of(base))
			}
		})
	}
	if same := base; Of(same) != Of(base) {
		t.Error("the same content hashed differently")
	}
	if got := len(Of(base)); got != 64 {
		t.Errorf("hash %q is %d characters, want 64 hex", Of(base), got)
	}
}
