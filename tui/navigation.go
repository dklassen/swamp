package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
)

// screenDeps is what a screen is opened with: the services, and the
// session's state as it is when the screen opens.
type screenDeps struct {
	companies    *service.CompanyService
	postings     *service.PostingService
	applications *service.ApplicationService
	documentSvc  *service.DocumentService
	scores       *service.ScoringService
	syncer       *sync.Syncer
	// documents resolves an application's document paths, hiding the
	// path convention and base directory the same way store hides
	// schema/SQL details -- threaded through from SWAMP_DOCUMENTS_PATH,
	// mirroring how SWAMP_DB_PATH configures the store.
	documents *documents.Store
	// openURL opens a URL in the browser: openInBrowser, swapped for a fake
	// in tests so they don't launch one.
	openURL func(src message.Source, about message.Subject, url string) tea.Cmd

	// sized is the last size sent to the screens.
	sized message.ContentSize
	// exportDir is the destination the export screen prefills: the last
	// directory successfully exported to this session, falling back to
	// defaultExportDir. Ephemeral and in-memory only, like hideArchived
	// -- a remembered path is a within-session convenience, not
	// something worth a schema change to persist.
	exportDir string
	// hideArchived is what each posting list opens with: the last one's
	// choice, for the session only. Defaults true: the point of archiving
	// a posting is to declutter the list.
	hideArchived bool
}

// opens has, for each kind a Nav[T] can name, the screen it opens.
type opens[T message.Body] map[message.Screen]func(screenDeps, T) stackScreen

// Navigation is which screen each Nav opens, one table per payload, so a
// screen given the wrong payload doesn't compile.
type Navigation struct {
	none              opens[message.None]
	company           opens[message.Company]
	filterSelection   opens[message.FilterSelection]
	postingDetail     opens[message.PostingDetail]
	application       opens[message.Application]
	documentReview    opens[message.DocumentReview]
	applicationDetail opens[message.ApplicationDetail]
}

// Screens is every screen a Nav opens. Any of them opens on a push or a
// replace; the replaces meant are posting detail giving way to the next
// posting, and the review form taking the picker's place.
var Screens = Navigation{
	none: opens[message.None]{
		message.ScreenCompanyForm: func(d screenDeps, _ message.None) stackScreen {
			return newCompanyFormModel(d.syncer)
		},
	},
	company: opens[message.Company]{
		message.ScreenCompanyEdit: func(d screenDeps, c message.Company) stackScreen {
			return newCompanyEditModel(d.companies, c.ID, c.Name)
		},
		message.ScreenCompanyPostings: func(d screenDeps, c message.Company) stackScreen {
			return newPostingListModel(d.companies, d.postings, c, d.hideArchived, d.sized)
		},
	},
	filterSelection: opens[message.FilterSelection]{
		message.ScreenFilterSelect: func(d screenDeps, f message.FilterSelection) stackScreen {
			return newFilterSelectModel(d.companies, f, d.sized)
		},
	},
	postingDetail: opens[message.PostingDetail]{
		message.ScreenPostingDetail: func(d screenDeps, p message.PostingDetail) stackScreen {
			return newPostingDetailModel(d.postings, d.documentSvc, d.scores, d.documents, p, d.sized)
		},
	},
	application: opens[message.Application]{
		message.ScreenApplicationStatus: func(d screenDeps, a message.Application) stackScreen {
			return newApplicationStatusModel(d.applications, a)
		},
		message.ScreenApplicationNotes: func(d screenDeps, a message.Application) stackScreen {
			return newApplicationNotesModel(d.applications, a, d.sized)
		},
		message.ScreenDocumentReviewSelect: func(d screenDeps, a message.Application) stackScreen {
			return newDocumentReviewSelectModel(d.documents, a)
		},
		message.ScreenApplicationForm: func(d screenDeps, a message.Application) stackScreen {
			return newApplicationFormModel(d.postings, a, d.sized)
		},
	},
	documentReview: opens[message.DocumentReview]{
		message.ScreenDocumentReviewForm: func(d screenDeps, r message.DocumentReview) stackScreen {
			return newDocumentReviewFormModel(d.documentSvc, d.documents, r, d.sized)
		},
	},
	applicationDetail: opens[message.ApplicationDetail]{
		message.ScreenApplicationDetail: func(d screenDeps, a message.ApplicationDetail) stackScreen {
			return newApplicationDetailModel(d.applications, d.postings, d.companies, d.documentSvc, d.documents, a, d.sized.Width)
		},
		message.ScreenApplicationExport: func(d screenDeps, a message.ApplicationDetail) stackScreen {
			return newApplicationExportModel(d.documentSvc, d.documents, a, d.exportDir, d.sized)
		},
		message.ScreenApplicationSubmit: func(d screenDeps, a message.ApplicationDetail) stackScreen {
			return newApplicationSubmitModel(d.applications, d.documentSvc, d.documents, d.openURL, a, d.exportDir)
		},
	},
}

// navTo opens what table has for each Nav[T] from the screen on top.
func navTo[T message.Body](a *App, table opens[T]) func(message.Event[message.Nav[T]]) tea.Cmd {
	return func(e message.Event[message.Nav[T]]) tea.Cmd {
		if !a.onTop(e.Meta) {
			return nil
		}
		nav := e.Payload
		if nav.Op == message.NavPop {
			return a.pop()
		}
		open, ok := table[nav.Screen]
		switch {
		case !ok:
			return a.unknownNav(e)
		case nav.Op == message.NavPush:
			return a.push(nav.Screen, open(a.deps, nav.Value))
		case nav.Op == message.NavReplace:
			return a.replace(nav.Screen, open(a.deps, nav.Value))
		}
		return a.unknownNav(e)
	}
}

// unknownNav reports a Nav for a screen App doesn't open that way.
func (a *App) unknownNav(nav tea.Msg) tea.Cmd {
	return a.fail(fmt.Errorf("tui: no screen opens on %+v", nav))
}
