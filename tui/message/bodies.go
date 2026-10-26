package message

import "time"

// Company is what an event carries for a company. DeletedAt is zero
// while it isn't deleted.
type Company struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	DeletedAt   time.Time `json:"deleted_at,omitzero"`
}

func (Company) Type() Type { return TypeCompany }

// ContentSize is the area a screen draws in: the terminal less the header
// and banner.
type ContentSize struct {
	Width int `json:"width"`
	Rows  int `json:"rows"`
}

func (ContentSize) Type() Type { return TypeContentSize }
