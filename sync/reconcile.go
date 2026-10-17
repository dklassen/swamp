package sync

import (
	"context"
	"fmt"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/jobboard"
)

// closeMissing reconciles the company's stored postings with the fetch:
// it records that the board still lists every stored posting it returned,
// then closes each open one it didn't, ending its application if that is
// still at one of earlyApplicationStatuses. "Missing" is decided from the
// fetch, not from what ingest saved. It stops at the first error.
func (s *Syncer) closeMissing(ctx context.Context, company company.Company, fetched []jobboard.Posting, result Result) (Result, error) {
	seenSourceIDs := make(map[string]bool, len(fetched))
	for _, p := range fetched {
		seenSourceIDs[p.SourceID] = true
	}

	existingPostings, err := s.postings.ListByCompany(ctx, company.ID)
	if err != nil {
		return result, fmt.Errorf("sync: list existing postings: %w", err)
	}
	var seen []int64
	for _, existing := range existingPostings {
		if seenSourceIDs[existing.SourceID] {
			seen = append(seen, existing.ID)
		}
	}
	// Every stored posting the board listed, including ones the filters
	// now leave out: last_seen_at is when the board last listed it (#176).
	if err := s.postings.MarkSeen(ctx, seen); err != nil {
		return result, fmt.Errorf("sync: mark postings seen: %w", err)
	}
	for _, existing := range existingPostings {
		if existing.ListingStatus != "open" || seenSourceIDs[existing.SourceID] {
			continue
		}
		if err := s.postings.Close(ctx, existing.ID, earlyApplicationStatuses); err != nil {
			return result, fmt.Errorf("sync: close posting: %w", err)
		}
		result.Closed++
	}
	return result, nil
}

// earlyApplicationStatuses are the statuses from which a posting being
// taken down ends the application. Deliberately not every non-terminal
// status: a company routinely pulls a listing while still interviewing
// the candidates already in its pipeline, so an application at
// interviewing or beyond is a live process that the syncer must not
// overwrite (see issue #105). Those are left alone.
// So is a submitted one (#174): a listing often comes down once the
// company has enough candidates, and the ones who applied are still
// being reviewed. Only an application never sent can no longer be.
//
// Closing a posting's application, and undoing that when the posting
// reappears (PostingService.Reopen), are the only places sync reaches
// past postings and posting history into application state, a deliberate
// widening of what a sync does. The policy stays
// here; PostingService.Close applies it in the same transaction that closes the
// posting.
var earlyApplicationStatuses = []application.Status{
	application.Started,
}
