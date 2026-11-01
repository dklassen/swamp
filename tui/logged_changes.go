package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// changeReader reads the change log's rows as the Changed events they
// record, so another process's write reaches a model the way its own does.
type changeReader struct {
	tables map[string]loggedTable
	// origin is this process's, whose rows its own commands already sent.
	origin string
	log    *service.ChangeLog
	// after is the reader's place in the log, and reading whether a read is
	// in flight.
	after    int64
	reading  bool
	instance message.ScreenInstance
	// poll is how long the reader waits between reads.
	poll time.Duration
}

// loggedTable reads a table's rows by ID, giving each row's event, or nil
// for a row whose entity is gone since.
type loggedTable func(ctx context.Context, ids []int64) (func(row change.Event, op message.Op) (tea.Msg, error), error)

func newChangeReader(companies *service.CompanyService, postings *service.PostingService, applications *service.ApplicationService, documents *service.DocumentService, scores *service.ScoringService, log *service.ChangeLog, origin string, since int64, poll time.Duration) *changeReader {
	return &changeReader{origin: origin, log: log, after: since, poll: poll, instance: message.NewScreenInstance(), tables: map[string]loggedTable{
		"companies":        logged(companies.GetManyIncludingDeleted, func(c company.Company) int64 { return c.ID }, func(c company.Company) bool { return c.DeletedAt != nil }, companySnapshot, rowSubject[message.Company](message.TypeCompany)),
		"applications":     logged(applications.GetManyIncludingDeleted, func(a application.Application) int64 { return a.ID }, func(a application.Application) bool { return !a.DeletedAt.IsZero() }, applicationSnapshot, rowSubject[message.Application](message.TypeApplication)),
		"postings":         logged(postings.GetMany, func(p posting.Posting) int64 { return p.ID }, nil, postingSnapshot, rowSubject[message.Posting](message.TypePosting)),
		"posting_markup":   logged(postings.MarkupMany, func(m posting.Markup) int64 { return m.PostingID }, nil, markupSnapshot, rowSubject[message.Markup](message.TypePosting)),
		"posting_scores":   logged(scores.GetMany, func(s score.Score) int64 { return s.ID }, nil, scoreSnapshot, scoreSubject),
		"document_reviews": logged(documents.GetManyReviews, func(r document.Review) int64 { return r.ID }, nil, reviewSnapshot, reviewSubject),
		"document_writes":  logged(documents.GetManyWrites, func(w document.Write) int64 { return w.ID }, nil, documentWriteSnapshot, documentWriteSubject),
		"document_exports": logged(documents.GetManyExports, func(e document.Export) int64 { return e.ID }, nil, documentExportSnapshot, documentExportSubject),
	}}
}

func (r *changeReader) Instance() message.ScreenInstance { return r.instance }

func (r *changeReader) Routes() []*model.Route {
	return []*model.Route{
		model.Always(r.tick, model.From(r.instance)),
		model.Always(r.pageRead, model.From(r.instance)),
		model.Always(r.readFailed, model.From(r.instance)),
	}
}

// Init asks for the first tick.
func (r *changeReader) Init() tea.Cmd { return r.next() }

// next asks for a tick after poll.
func (r *changeReader) next() tea.Cmd {
	src := message.Source{Instance: r.instance}
	return tea.Tick(r.poll, func(at time.Time) tea.Msg {
		return message.New(src, message.Subject{}, message.OpNone, at.UTC(), message.Signal[message.ChangeTick]{})
	})
}

// tick asks for the next tick, and reads the log unless a read is still in
// flight.
func (r *changeReader) tick(message.Event[message.Signal[message.ChangeTick]]) tea.Cmd {
	if r.reading {
		return r.next()
	}
	r.reading = true
	return tea.Batch(r.next(), r.read())
}

// read sends the events of the log's rows after the reader's place, in the
// log's order, then the place after them. A Sequence, since a command
// sends one message and the router routes each event by its type.
func (r *changeReader) read() tea.Cmd {
	src, after := message.Source{Instance: r.instance}, r.after
	return func() tea.Msg {
		ctx := context.Background()
		failed := func(err error) tea.Msg {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		rows, err := r.log.After(ctx, after)
		if err != nil {
			return failed(err)
		}
		events, err := r.changed(ctx, rows)
		if err != nil {
			return failed(err)
		}
		page := message.ChangePage{After: after}
		if n := len(rows); n > 0 {
			page.After = rows[n-1].ID
		}
		cmds := make([]tea.Cmd, 0, len(events)+1)
		for _, e := range events {
			cmds = append(cmds, func() tea.Msg { return e })
		}
		cmds = append(cmds, func() tea.Msg {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Read[message.ChangePage]{Value: page})
		})
		return tea.Sequence(cmds...)()
	}
}

// pageRead moves the reader's place past the events just sent.
func (r *changeReader) pageRead(e message.Event[message.Read[message.ChangePage]]) tea.Cmd {
	r.reading, r.after = false, e.Payload.Value.After
	return nil
}

// readFailed lets the next tick read from the same place. The banner shows
// the error.
func (r *changeReader) readFailed(message.Event[message.Error]) tea.Cmd {
	r.reading = false
	return nil
}

// changed is each of rows not from origin as its Changed, in the log's
// order, with After read now: once per table, since a sync can log
// hundreds of rows.
func (r *changeReader) changed(ctx context.Context, rows []change.Event) ([]tea.Msg, error) {
	rows = slices.DeleteFunc(slices.Clone(rows), func(e change.Event) bool { return e.Origin == r.origin })
	ids := make(map[string][]int64)
	for _, row := range rows {
		if _, ok := r.tables[row.Table]; !ok {
			return nil, fmt.Errorf("change log: no event for table %q", row.Table)
		}
		ids[row.Table] = append(ids[row.Table], row.RowID)
	}
	eventOf := make(map[string]func(change.Event, message.Op) (tea.Msg, error), len(ids))
	for table, tableIDs := range ids {
		read, err := r.tables[table](ctx, tableIDs)
		if err != nil {
			return nil, fmt.Errorf("change log: read %s: %w", table, err)
		}
		eventOf[table] = read
	}

	events := make([]tea.Msg, 0, len(rows))
	for _, row := range rows {
		op, err := loggedOp(row.Op)
		if err != nil {
			return nil, err
		}
		e, err := eventOf[row.Table](row, op)
		if err != nil {
			return nil, err
		}
		if e != nil {
			events = append(events, e)
		}
	}
	return events, nil
}

// logged is the table read and keyed by read and key, its rows' events
// made with snapshot and subject. A delete's After is zero, and a row
// whose entity isn't found is left out, since its delete is a later row.
// deleted, nil for a table with no soft delete, makes an update to a row
// now soft deleted a delete, Before the row as it's left, so it reads as
// the TUI's own delete does, and leaves out its insert, as gone. An update
// from soft deleted to live is a restore, so a create, as the TUI's own is.
func logged[D any, S message.Snapshot](read func(context.Context, []int64) ([]D, error), key func(D) int64, deleted func(D) bool, snapshot func(D) S, subject func(change.Event, S) message.Subject) loggedTable {
	return func(ctx context.Context, ids []int64) (func(change.Event, message.Op) (tea.Msg, error), error) {
		values, err := read(ctx, ids)
		if err != nil {
			return nil, err
		}
		found := make(map[int64]D, len(values))
		for _, v := range values {
			found[key(v)] = v
		}
		return func(row change.Event, op message.Op) (tea.Msg, error) {
			src := message.Source{Process: row.Origin}
			var after S
			if op != message.OpDeleted {
				d, ok := found[row.RowID]
				if !ok {
					return nil, nil
				}
				if deleted != nil && deleted(d) {
					if op == message.OpCreated {
						return nil, nil
					}
					e := message.Logged(row.ID, src, subject(row, snapshot(d)), message.OpDeleted, row.At, after)
					e.Payload.Before = snapshot(d)
					return e, nil
				}
				if deleted != nil && op == message.OpUpdated {
					restored, err := wasDeleted(row)
					if err != nil {
						return nil, err
					}
					if restored {
						op = message.OpCreated
					}
				}
				after = snapshot(d)
			}
			return message.Logged(row.ID, src, subject(row, after), op, row.At, after), nil
		}, nil
	}
}

// wasDeleted is whether the row was soft deleted before its update.
func wasDeleted(row change.Event) (bool, error) {
	var old struct {
		DeletedAt string `json:"deleted_at"`
	}
	if err := json.Unmarshal([]byte(row.Old), &old); err != nil {
		return false, fmt.Errorf("change log: row %d's old %s: %w", row.ID, row.Table, err)
	}
	return old.DeletedAt != "", nil
}

// rowSubject is a Subject of type t named by the row's ID.
func rowSubject[S message.Snapshot](t message.Type) func(change.Event, S) message.Subject {
	return func(row change.Event, _ S) message.Subject { return message.Subject{Type: t, ID: row.RowID} }
}

// A score's Subject is its posting, and a review's, write's or export's its
// application, since the screens that show them show them by that entity.
func scoreSubject(_ change.Event, s message.Score) message.Subject {
	return message.Subject{Type: message.TypePosting, ID: s.PostingID}
}

func reviewSubject(_ change.Event, r message.Review) message.Subject {
	return message.Subject{Type: message.TypeApplication, ID: r.ApplicationID}
}

func documentWriteSubject(_ change.Event, w message.DocumentWrite) message.Subject {
	return message.Subject{Type: message.TypeApplication, ID: w.ApplicationID}
}

func documentExportSubject(_ change.Event, e message.DocumentExport) message.Subject {
	return message.Subject{Type: message.TypeApplication, ID: e.ApplicationID}
}

func loggedOp(op string) (message.Op, error) {
	switch op {
	case "insert":
		return message.OpCreated, nil
	case "update":
		return message.OpUpdated, nil
	case "delete":
		return message.OpDeleted, nil
	}
	return message.OpNone, fmt.Errorf("change log: unknown op %q", op)
}
