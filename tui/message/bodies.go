package message

import "time"

// Company is what an event carries for a company. LastFetchedAt is zero
// until its postings are first fetched, and DeletedAt while it isn't
// deleted.
type Company struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Source        string    `json:"source"`
	SourceRef     string    `json:"source_ref"`
	Description   string    `json:"description"`
	LastFetchedAt time.Time `json:"last_fetched_at,omitzero"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	DeletedAt     time.Time `json:"deleted_at,omitzero"`
}

func (Company) Type() Type { return TypeCompany }

// ContentSize is the area a screen draws in: the terminal less the header
// and banner.
type ContentSize struct {
	Width int `json:"width"`
	Rows  int `json:"rows"`
}

func (ContentSize) Type() Type { return TypeContentSize }

// FocusChanged names the model now in focus, 0 when none is.
type FocusChanged struct {
	Focused ScreenInstance `json:"focused"`
}

func (FocusChanged) Type() Type { return TypeFocusChanged }
