package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/store"
)

// QueuedPosting is a posting with drafting still to do.
type QueuedPosting struct {
	Posting     posting.Posting
	CompanyName string
	// Application is nil until drafting starts.
	Application *application.Application
	// LatestReviews holds each type's latest review while it describes
	// the document on disk.
	LatestReviews map[document.Type]document.Review
}

// Queue returns the postings with drafting still to do.
func (service *DraftingService) Queue(ctx context.Context) ([]QueuedPosting, error) {
	var interested []posting.Interested
	var active []posting.ActiveApplication
	var reviews map[int64]map[document.Type]document.Review
	var markups []posting.Markup
	// One transaction, so every interested posting's application is among
	// the active ones.
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		var err error
		if interested, err = service.postingViewer.ListInterested(ctx, tx, application.Terminal()); err != nil {
			return fmt.Errorf("interested postings: %w", err)
		}
		if active, err = service.applications.ListActive(ctx, tx, application.Terminal()); err != nil {
			return fmt.Errorf("active applications: %w", err)
		}
		ids := make([]int64, len(active))
		postingIDs := make([]int64, len(active))
		for i, a := range active {
			ids[i], postingIDs[i] = a.ID, a.Posting.ID
		}
		if reviews, err = service.reviewViewer.LatestMany(ctx, tx, ids); err != nil {
			return fmt.Errorf("latest reviews: %w", err)
		}
		if markups, err = service.markupViewer.GetMany(ctx, tx, postingIDs); err != nil {
			return fmt.Errorf("markups: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("drafting: queue: %w", err)
	}
	applications := make(map[int64]application.Application, len(active))
	for _, a := range active {
		applications[a.Posting.ID] = a.Application
	}

	archived := make(map[int64]bool, len(markups))
	for _, m := range markups {
		archived[m.PostingID] = m.ArchivedAt != nil
	}

	queue := make([]QueuedPosting, 0, len(interested))
	queued := make(map[int64]bool, len(interested))
	for _, p := range interested {
		queued[p.Posting.ID] = true
		entry := QueuedPosting{Posting: p.Posting, CompanyName: p.CompanyName}
		if p.ApplicationID != nil {
			app, ok := applications[p.Posting.ID]
			if !ok {
				return nil, fmt.Errorf("drafting: queue: application %d of posting %d isn't active", *p.ApplicationID, p.Posting.ID)
			}
			entry.Application = &app
		}
		if queue, err = service.appendUndrafted(queue, entry, reviews); err != nil {
			return nil, err
		}
	}

	slices.SortFunc(active, func(a, b posting.ActiveApplication) int {
		return cmp.Or(compareCompany(a, b), cmp.Compare(a.ID, b.ID))
	})
	for _, a := range active {
		if a.Status != application.Started || queued[a.Posting.ID] || archived[a.Posting.ID] {
			continue
		}
		entry := QueuedPosting{Posting: a.Posting, CompanyName: a.CompanyName, Application: &a.Application}
		if queue, err = service.appendUndrafted(queue, entry, reviews); err != nil {
			return nil, err
		}
	}
	return queue, nil
}

// appendUndrafted appends entry to queue unless its application has every
// document drafted and none flagged, setting its current reviews.
func (service *DraftingService) appendUndrafted(queue []QueuedPosting, entry QueuedPosting, reviews map[int64]map[document.Type]document.Review) ([]QueuedPosting, error) {
	if entry.Application == nil {
		return append(queue, entry), nil
	}
	id := entry.Application.ID
	status := service.documents.Status(id)
	current, err := documents.Current(status, reviews[id], document.Review.IsCurrent)
	if err != nil {
		return nil, fmt.Errorf("drafting: queue: current reviews of application %d: %w", id, err)
	}
	if allDrafted(status) && !document.NeedsRework(current) {
		return queue, nil
	}
	entry.LatestReviews = current
	return append(queue, entry), nil
}

// allDrafted reports whether every document type exists on disk.
func allDrafted(status documents.Status) bool {
	for _, documentType := range document.Types() {
		doc, err := status.Doc(documentType)
		if err != nil || !doc.Exists {
			return false
		}
	}
	return true
}
