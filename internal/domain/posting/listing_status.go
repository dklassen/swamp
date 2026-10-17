package posting

import (
	"encoding/json"
	"fmt"
)

// ListingStatus is whether the board still lists a posting.
type ListingStatus int

const (
	ListingOpen ListingStatus = iota
	ListingClosed
)

// listingStatusNames is the postings.listing_status value of each status,
// the one mapping String and ParseListingStatus share.
var listingStatusNames = [...]string{
	ListingOpen:   "open",
	ListingClosed: "closed",
}

func (s ListingStatus) String() string {
	if s < 0 || int(s) >= len(listingStatusNames) {
		return fmt.Sprintf("ListingStatus(%d)", int(s))
	}
	return listingStatusNames[s]
}

func (s ListingStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *ListingStatus) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	parsed, err := ParseListingStatus(name)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// ParseListingStatus reads a postings.listing_status value, failing on one
// it doesn't know rather than guessing.
func ParseListingStatus(s string) (ListingStatus, error) {
	for i, name := range listingStatusNames {
		if name == s {
			return ListingStatus(i), nil
		}
	}
	return 0, fmt.Errorf("posting: unknown listing status %q", s)
}
