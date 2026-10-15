package application

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/store"
)

// EndForPosting moves postingID's application, if it has one at a status
// in from, to posting_closed, recorded as sync's change. Which statuses a
// closing posting ends is the caller's policy. ended is false when there
// is no application or its status isn't in from.
func EndForPosting(ctx context.Context, tx store.Tx, postingID int64, from []applicationstatus.Status) (ended bool, err error) {
	err = store.InTx(ctx, tx, func(tx store.Tx) error {
		app, err := Get(ctx, tx, postingID)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !slices.Contains(from, app.Status) {
			return nil
		}
		if _, err := setStatus(ctx, tx, postingID, applicationstatus.PostingClosed); err != nil {
			return err
		}
		ended = true
		return recordStatus(ctx, tx, app.ID, applicationstatus.PostingClosed, ChangedBySync)
	})
	if err != nil {
		return false, fmt.Errorf("application: end for closed posting %d: %w", postingID, err)
	}
	return ended, nil
}

// RestoreForPosting undoes what EndForPosting did when the posting
// reopens: an application sync moved to posting_closed goes back to the
// status it had before, recorded as sync's change. Only if sync's close is
// still the application's latest change -- a posting_closed the user set,
// or any status the user set since, is left alone, and so is a close
// recorded before history said who made it.
func RestoreForPosting(ctx context.Context, tx store.Tx, postingID int64) (restored bool, err error) {
	err = store.InTx(ctx, tx, func(tx store.Tx) error {
		app, err := Get(ctx, tx, postingID)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		history, err := ListStatusHistory(ctx, tx, app.ID)
		if err != nil {
			return err
		}
		if len(history) < 2 {
			return nil
		}
		last := history[len(history)-1]
		if app.Status != applicationstatus.PostingClosed ||
			last.Status != applicationstatus.PostingClosed ||
			last.ChangedBy != ChangedBySync {
			return nil
		}
		previous := history[len(history)-2].Status
		if _, err := setStatus(ctx, tx, postingID, previous); err != nil {
			return err
		}
		restored = true
		return recordStatus(ctx, tx, app.ID, previous, ChangedBySync)
	})
	if err != nil {
		return false, fmt.Errorf("application: restore for reopened posting %d: %w", postingID, err)
	}
	return restored, nil
}
