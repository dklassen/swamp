package tui

import (
	"context"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
)

// changeReader reads the change log's rows as the Changed events they
// record, so another process's write reaches a model the way its own does.
type changeReader struct {
	tables map[string]loggedTable
	// origin is this process's, whose rows its own commands already sent.
	origin string
}

// loggedTable reads a table's rows by ID, giving each row's event, or nil
// for a row whose entity is gone since.
type loggedTable func(ctx context.Context, ids []int64) (func(row change.Event, op message.Op) tea.Msg, error)

func newChangeReader(companies *service.CompanyService, postings *service.PostingService, applications *service.ApplicationService, documents *service.DocumentService, scores *service.ScoringService, origin string) *changeReader {
	return &changeReader{origin: origin, tables: map[string]loggedTable{
		"companies":        logged(companies.GetManyIncludingDeleted, func(c company.Company) int64 { return c.ID }, companySnapshot, rowSubject[message.Company](message.TypeCompany)),
		"applications":     logged(applications.GetManyIncludingDeleted, func(a application.Application) int64 { return a.ID }, applicationSnapshot, rowSubject[message.Application](message.TypeApplication)),
		"postings":         logged(postings.GetMany, func(p posting.Posting) int64 { return p.ID }, postingSnapshot, rowSubject[message.Posting](message.TypePosting)),
		"posting_markup":   logged(postings.MarkupMany, func(m posting.Markup) int64 { return m.PostingID }, markupSnapshot, rowSubject[message.Markup](message.TypePosting)),
		"posting_scores":   logged(scores.GetMany, func(s score.Score) int64 { return s.ID }, scoreSnapshot, scoreSubject),
		"document_reviews": logged(documents.GetManyReviews, func(r document.Review) int64 { return r.ID }, reviewSnapshot, reviewSubject),
		"document_writes":  logged(documents.GetManyWrites, func(w document.Write) int64 { return w.ID }, documentWriteSnapshot, documentWriteSubject),
		"document_exports": logged(documents.GetManyExports, func(e document.Export) int64 { return e.ID }, documentExportSnapshot, documentExportSubject),
	}}
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
	eventOf := make(map[string]func(change.Event, message.Op) tea.Msg, len(ids))
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
		if e := eventOf[row.Table](row, op); e != nil {
			events = append(events, e)
		}
	}
	return events, nil
}

// logged is the table read and keyed by read and key, its rows' events
// made with snapshot and subject. A delete's After is zero, and a row
// whose entity isn't found is left out, since its delete is a later row.
func logged[D any, S message.Snapshot](read func(context.Context, []int64) ([]D, error), key func(D) int64, snapshot func(D) S, subject func(change.Event, S) message.Subject) loggedTable {
	return func(ctx context.Context, ids []int64) (func(change.Event, message.Op) tea.Msg, error) {
		values, err := read(ctx, ids)
		if err != nil {
			return nil, err
		}
		found := make(map[int64]D, len(values))
		for _, v := range values {
			found[key(v)] = v
		}
		return func(row change.Event, op message.Op) tea.Msg {
			var after S
			if op != message.OpDeleted {
				d, ok := found[row.RowID]
				if !ok {
					return nil
				}
				after = snapshot(d)
			}
			return message.Logged(row.ID, message.Source{Process: row.Origin}, subject(row, after), op, row.At, after)
		}, nil
	}
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
