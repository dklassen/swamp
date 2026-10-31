package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

func toggleInterested(postings *service.PostingService, src message.Source, postingID int64, currentlyInterested bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var (
			m   *change.Of[posting.Markup]
			err error
		)
		if currentlyInterested {
			m, err = postings.UnmarkInterested(ctx, postingID)
		} else {
			m, err = postings.SetInterested(ctx, postingID)
		}
		return markupChanged(src, postingID, m, err)
	}
}

func toggleArchived(postings *service.PostingService, src message.Source, postingID int64, currentlyArchived bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var (
			m   *change.Of[posting.Markup]
			err error
		)
		if currentlyArchived {
			m, err = postings.Unarchive(ctx, postingID)
		} else {
			m, err = postings.SetArchived(ctx, postingID)
		}
		return markupChanged(src, postingID, m, err)
	}
}

func markupChanged(src message.Source, postingID int64, m *change.Of[posting.Markup], err error) tea.Msg {
	subject := message.Subject{Type: message.TypePosting, ID: postingID}
	if err != nil {
		return message.New(src, subject, message.OpUpdated, time.Now().UTC(), model.ErrorOf(err))
	}
	return message.New(src, subject, message.OpUpdated, m.At,
		message.Changed[message.Markup]{Before: markupSnapshot(m.Before), After: markupSnapshot(m.After)})
}

// createApplication's error is about the posting, since there's no
// application yet.
func createApplication(postings *service.PostingService, src message.Source, postingID int64) tea.Cmd {
	return func() tea.Msg {
		app, err := postings.ApplyTo(context.Background(), postingID)
		if err != nil {
			return message.New(src, message.Subject{Type: message.TypePosting, ID: postingID}, message.OpCreated, time.Now().UTC(), model.ErrorOf(err))
		}
		return message.Created(src, app.ID, app.CreatedAt, applicationSnapshot(*app))
	}
}

func applicationWritten(src message.Source, applicationID int64, op message.Op, a *change.Of[application.Application], err error) tea.Msg {
	if err != nil {
		return message.New(src, message.Subject{Type: message.TypeApplication, ID: applicationID}, op, time.Now().UTC(), model.ErrorOf(err))
	}
	if op == message.OpDeleted {
		return message.Deleted(src, applicationID, a.At, applicationSnapshot(a.Before))
	}
	return message.Updated(src, applicationID, a.At, applicationSnapshot(a.Before), applicationSnapshot(a.After))
}

// currentReviews is the latest review of each of an application's
// documents, leaving out one whose document has changed since.
func currentReviews(documentService *service.DocumentService, docs *documents.Store, applicationID int64) (map[document.Type]document.Review, error) {
	reviews, err := documentService.LatestReviews(context.Background(), applicationID)
	if err != nil {
		return nil, err
	}
	return documents.Current(docs.Status(applicationID), reviews, document.Review.IsCurrent)
}

func updateApplicationStatus(applications *service.ApplicationService, src message.Source, applicationID int64, status application.Status) tea.Cmd {
	return func() tea.Msg {
		a, err := applications.SetStatus(context.Background(), applicationID, status)
		return applicationWritten(src, applicationID, message.OpUpdated, a, err)
	}
}
