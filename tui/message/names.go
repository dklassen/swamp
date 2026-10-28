package message

import "fmt"

// Kind, Type, Op, NavOp, ErrorCode and Screen are decided on, so Go code
// compares constants; each encodes as its name from one table, so
// reordering the constants can't change what a stored event means. A
// zero value is in its table only where "none" is legal.

type Kind int

const (
	_ Kind = iota
	KindChanged
	KindRead
	KindLoad
	KindRun
	KindRan
	KindError
	KindNav
	KindNotify
	KindSignal
)

var kindNames = [...]string{
	KindChanged: "changed",
	KindRead:    "read",
	KindLoad:    "load",
	KindRun:     "run",
	KindRan:     "ran",
	KindError:   "error",
	KindNav:     "nav",
	KindNotify:  "notify",
	KindSignal:  "signal",
}

func (k Kind) String() string                { return nameOf(k, kindNames[:]) }
func (k Kind) MarshalText() ([]byte, error)  { return marshalName(k, kindNames[:]) }
func (k *Kind) UnmarshalText(b []byte) error { return unmarshalName(k, b, kindNames[:]) }

// Type is what a payload is about within its kind.
type Type int

const (
	_ Type = iota
	TypeCompany
	TypeCompanyList
	TypeCompanyRow
	TypeSyncCompany
	TypeSyncAll
	TypeSyncReport
	TypeSyncAllReport
	TypeContentSize
	TypeFocusChanged
	TypeStatus
	TypeError
	TypeNone
)

var typeNames = [...]string{
	TypeCompany:       "company",
	TypeCompanyList:   "company_list",
	TypeCompanyRow:    "company_row",
	TypeSyncCompany:   "sync_company",
	TypeSyncAll:       "sync_all",
	TypeSyncReport:    "sync_report",
	TypeSyncAllReport: "sync_all_report",
	TypeContentSize:   "content_size",
	TypeFocusChanged:  "focus_changed",
	TypeStatus:        "status",
	TypeError:         "error",
	TypeNone:          "none",
}

func (t Type) String() string                { return nameOf(t, typeNames[:]) }
func (t Type) MarshalText() ([]byte, error)  { return marshalName(t, typeNames[:]) }
func (t *Type) UnmarshalText(b []byte) error { return unmarshalName(t, b, typeNames[:]) }

// Op is what a changed event wrote, or what an error event didn't. A
// request has OpNone.
type Op int

const (
	OpNone Op = iota
	OpCreated
	OpUpdated
	OpDeleted
)

var opNames = [...]string{
	OpNone:    "none",
	OpCreated: "created",
	OpUpdated: "updated",
	OpDeleted: "deleted",
}

func (o Op) String() string                { return nameOf(o, opNames[:]) }
func (o Op) MarshalText() ([]byte, error)  { return marshalName(o, opNames[:]) }
func (o *Op) UnmarshalText(b []byte) error { return unmarshalName(o, b, opNames[:]) }

type NavOp int

const (
	_ NavOp = iota
	NavPush
	NavPop
	NavReplace
)

var navOpNames = [...]string{
	NavPush:    "push",
	NavPop:     "pop",
	NavReplace: "replace",
}

func (o NavOp) String() string                { return nameOf(o, navOpNames[:]) }
func (o NavOp) MarshalText() ([]byte, error)  { return marshalName(o, navOpNames[:]) }
func (o *NavOp) UnmarshalText(b []byte) error { return unmarshalName(o, b, navOpNames[:]) }

type ErrorCode int

const (
	_ ErrorCode = iota
	ErrNotFound
	ErrSyncInProgress
	ErrOther
	ErrFault
)

var errorCodeNames = [...]string{
	ErrNotFound:       "not_found",
	ErrSyncInProgress: "sync_in_progress",
	ErrOther:          "other",
	ErrFault:          "fault",
}

func (c ErrorCode) String() string                { return nameOf(c, errorCodeNames[:]) }
func (c ErrorCode) MarshalText() ([]byte, error)  { return marshalName(c, errorCodeNames[:]) }
func (c *ErrorCode) UnmarshalText(b []byte) error { return unmarshalName(c, b, errorCodeNames[:]) }

// Screen is a kind of screen a Nav opens. A pop opens none.
type Screen int

const (
	ScreenNone Screen = iota
	ScreenCompanyForm
	ScreenCompanyEdit
	ScreenCompanyDelete
	ScreenCompanyPostings
)

var screenNames = [...]string{
	ScreenNone:            "none",
	ScreenCompanyForm:     "company_form",
	ScreenCompanyEdit:     "company_edit",
	ScreenCompanyDelete:   "company_delete",
	ScreenCompanyPostings: "company_postings",
}

func (s Screen) String() string                { return nameOf(s, screenNames[:]) }
func (s Screen) MarshalText() ([]byte, error)  { return marshalName(s, screenNames[:]) }
func (s *Screen) UnmarshalText(b []byte) error { return unmarshalName(s, b, screenNames[:]) }

// An empty entry is a value with no name, such as the zero Kind.
func nameOf[E ~int](e E, names []string) string {
	if e < 0 || int(e) >= len(names) || names[e] == "" {
		return fmt.Sprintf("%T(%d)", e, int(e))
	}
	return names[e]
}

func marshalName[E ~int](e E, names []string) ([]byte, error) {
	if e < 0 || int(e) >= len(names) || names[e] == "" {
		return nil, fmt.Errorf("message: %T(%d) has no name", e, int(e))
	}
	return []byte(names[e]), nil
}

func unmarshalName[E ~int](e *E, b []byte, names []string) error {
	for i, name := range names {
		if name != "" && name == string(b) {
			*e = E(i)
			return nil
		}
	}
	return fmt.Errorf("message: unknown %T %q", *e, b)
}
