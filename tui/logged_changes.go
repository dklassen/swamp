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
// record.
type changeReader struct {
	companies    *service.CompanyService
	postings     *service.PostingService
	applications *service.ApplicationService
	documents    *service.DocumentService
	scores       *service.ScoringService
	// origin is this process's, whose rows its own commands already sent.
	origin string
}

// changed is each of rows not from origin as its Changed, in the log's
// order, with After read now: once per table, since a sync can log
// hundreds of rows. A row whose entity is gone since is left out; its
// delete is a later row.
func (r changeReader) changed(ctx context.Context, rows []change.Event) ([]tea.Msg, error) {
	rows = slices.DeleteFunc(slices.Clone(rows), func(e change.Event) bool { return e.Origin == r.origin })
	ids := make(map[string][]int64)
	for _, row := range rows {
		if row.Op != "delete" {
			ids[row.Table] = append(ids[row.Table], row.RowID)
		}
	}
	companies, err := byID(ctx, ids["companies"], r.companies.GetManyIncludingDeleted, func(c company.Company) int64 { return c.ID })
	if err != nil {
		return nil, fmt.Errorf("change log: read companies: %w", err)
	}
	applications, err := byID(ctx, ids["applications"], r.applications.GetManyIncludingDeleted, func(a application.Application) int64 { return a.ID })
	if err != nil {
		return nil, fmt.Errorf("change log: read applications: %w", err)
	}
	postings, err := byID(ctx, ids["postings"], r.postings.GetMany, func(p posting.Posting) int64 { return p.ID })
	if err != nil {
		return nil, fmt.Errorf("change log: read postings: %w", err)
	}
	markup, err := byID(ctx, ids["posting_markup"], r.postings.MarkupMany, func(m posting.Markup) int64 { return m.PostingID })
	if err != nil {
		return nil, fmt.Errorf("change log: read markup: %w", err)
	}
	scores, err := byID(ctx, ids["posting_scores"], r.scores.GetMany, func(s score.Score) int64 { return s.ID })
	if err != nil {
		return nil, fmt.Errorf("change log: read scores: %w", err)
	}
	reviews, err := byID(ctx, ids["document_reviews"], r.documents.GetManyReviews, func(r document.Review) int64 { return r.ID })
	if err != nil {
		return nil, fmt.Errorf("change log: read reviews: %w", err)
	}
	writes, err := byID(ctx, ids["document_writes"], r.documents.GetManyWrites, func(w document.Write) int64 { return w.ID })
	if err != nil {
		return nil, fmt.Errorf("change log: read writes: %w", err)
	}
	exports, err := byID(ctx, ids["document_exports"], r.documents.GetManyExports, func(e document.Export) int64 { return e.ID })
	if err != nil {
		return nil, fmt.Errorf("change log: read exports: %w", err)
	}

	events := make([]tea.Msg, 0, len(rows))
	for _, row := range rows {
		op, err := loggedOp(row.Op)
		if err != nil {
			return nil, err
		}
		switch row.Table {
		case "companies":
			events = appendLogged(events, row, op, companies, companySnapshot, func(message.Company) message.Subject {
				return message.Subject{Type: message.TypeCompany, ID: row.RowID}
			})
		case "applications":
			events = appendLogged(events, row, op, applications, applicationSnapshot, func(message.Application) message.Subject {
				return message.Subject{Type: message.TypeApplication, ID: row.RowID}
			})
		case "postings":
			events = appendLogged(events, row, op, postings, postingSnapshot, func(message.Posting) message.Subject {
				return message.Subject{Type: message.TypePosting, ID: row.RowID}
			})
		case "posting_markup":
			events = appendLogged(events, row, op, markup, markupSnapshot, func(message.Markup) message.Subject {
				return message.Subject{Type: message.TypePosting, ID: row.RowID}
			})
		case "posting_scores":
			events = appendLogged(events, row, op, scores, scoreSnapshot, func(s message.Score) message.Subject {
				return message.Subject{Type: message.TypePosting, ID: s.PostingID}
			})
		case "document_reviews":
			events = appendLogged(events, row, op, reviews, reviewSnapshot, func(r message.Review) message.Subject {
				return message.Subject{Type: message.TypeApplication, ID: r.ApplicationID}
			})
		case "document_writes":
			events = appendLogged(events, row, op, writes, documentWriteSnapshot, func(w message.DocumentWrite) message.Subject {
				return message.Subject{Type: message.TypeApplication, ID: w.ApplicationID}
			})
		case "document_exports":
			events = appendLogged(events, row, op, exports, documentExportSnapshot, func(e message.DocumentExport) message.Subject {
				return message.Subject{Type: message.TypeApplication, ID: e.ApplicationID}
			})
		default:
			return nil, fmt.Errorf("change log: no event for table %q", row.Table)
		}
	}
	return events, nil
}

// byID reads ids with read, by the ID key gives each; none for no ids.
func byID[T any](ctx context.Context, ids []int64, read func(context.Context, []int64) ([]T, error), key func(T) int64) (map[int64]T, error) {
	found := make(map[int64]T, len(ids))
	if len(ids) == 0 {
		return found, nil
	}
	values, err := read(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, v := range values {
		found[key(v)] = v
	}
	return found, nil
}

// appendLogged appends row's event to events: After is the entity found
// by its row ID, zero for a delete, and row is left out if it isn't found.
func appendLogged[D any, S message.Snapshot](events []tea.Msg, row change.Event, op message.Op, found map[int64]D, snapshot func(D) S, subject func(S) message.Subject) []tea.Msg {
	var after S
	if op != message.OpDeleted {
		d, ok := found[row.RowID]
		if !ok {
			return events
		}
		after = snapshot(d)
	}
	return append(events, message.Logged(row.ID, message.Source{Process: row.Origin}, subject(after), op, row.At, after))
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
