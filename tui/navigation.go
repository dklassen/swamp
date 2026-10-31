package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// opens has, for each kind a Nav[T] can name, the screen it opens.
type opens[T message.Body] map[message.Screen]func(T) stackScreen

// Navigation builds the screen each Nav names, from the services it's
// built with and the session's state, which it keeps from the messages
// that carry it. It has a table per payload, so a screen given the wrong
// payload doesn't compile. Any screen in a table opens on a push or a
// replace; the replaces meant are posting detail giving way to the next
// posting, and the review form taking the picker's place.
type Navigation struct {
	instance message.ScreenInstance

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

	none              opens[message.None]
	company           opens[message.Company]
	filterSelection   opens[message.FilterSelection]
	postingDetail     opens[message.PostingDetail]
	application       opens[message.Application]
	documentReview    opens[message.DocumentReview]
	applicationDetail opens[message.ApplicationDetail]
}

// defaultExportDir is where the export screen points before anything has
// been exported this session. Configured here rather than via the
// environment or a flag: it's a starting point the user edits in the
// prompt whenever they want elsewhere, so a code-level default is the
// whole configuration surface it needs. A leading "~" is expanded when
// the export actually runs (see expandPath).
const defaultExportDir = "~/Desktop"

func NewNavigation(
	companies *service.CompanyService,
	postings *service.PostingService,
	applications *service.ApplicationService,
	documentService *service.DocumentService,
	scores *service.ScoringService,
	syncer *sync.Syncer,
	docs *documents.Store,
) *Navigation {
	n := &Navigation{
		instance:     message.NewScreenInstance(),
		companies:    companies,
		postings:     postings,
		applications: applications,
		documentSvc:  documentService,
		scores:       scores,
		syncer:       syncer,
		documents:    docs,
		openURL:      openInBrowser,
		exportDir:    defaultExportDir,
		hideArchived: true,
	}
	n.none = opens[message.None]{
		message.ScreenCompanyForm: func(message.None) stackScreen {
			return newCompanyFormModel(n.syncer)
		},
	}
	n.company = opens[message.Company]{
		message.ScreenCompanyEdit: func(c message.Company) stackScreen {
			return newCompanyEditModel(n.companies, c.ID, c.Name)
		},
		message.ScreenCompanyPostings: func(c message.Company) stackScreen {
			return newPostingListModel(n.companies, n.postings, c, n.hideArchived, n.sized)
		},
	}
	n.filterSelection = opens[message.FilterSelection]{
		message.ScreenFilterSelect: func(f message.FilterSelection) stackScreen {
			return newFilterSelectModel(n.companies, f, n.sized)
		},
	}
	n.postingDetail = opens[message.PostingDetail]{
		message.ScreenPostingDetail: func(p message.PostingDetail) stackScreen {
			return newPostingDetailModel(n.postings, n.documentSvc, n.scores, n.documents, p, n.sized)
		},
	}
	n.application = opens[message.Application]{
		message.ScreenApplicationStatus: func(a message.Application) stackScreen {
			return newApplicationStatusModel(n.applications, a)
		},
		message.ScreenApplicationNotes: func(a message.Application) stackScreen {
			return newApplicationNotesModel(n.applications, a, n.sized)
		},
		message.ScreenDocumentReviewSelect: func(a message.Application) stackScreen {
			return newDocumentReviewSelectModel(n.documents, a)
		},
		message.ScreenApplicationForm: func(a message.Application) stackScreen {
			return newApplicationFormModel(n.postings, a, n.sized)
		},
	}
	n.documentReview = opens[message.DocumentReview]{
		message.ScreenDocumentReviewForm: func(r message.DocumentReview) stackScreen {
			return newDocumentReviewFormModel(n.documentSvc, n.documents, r, n.sized)
		},
	}
	n.applicationDetail = opens[message.ApplicationDetail]{
		message.ScreenApplicationDetail: func(a message.ApplicationDetail) stackScreen {
			return newApplicationDetailModel(n.applications, n.postings, n.companies, n.documentSvc, n.documents, a, n.sized.Width)
		},
		message.ScreenApplicationExport: func(a message.ApplicationDetail) stackScreen {
			return newApplicationExportModel(n.documentSvc, n.documents, a, n.exportDir, n.sized)
		},
		message.ScreenApplicationSubmit: func(a message.ApplicationDetail) stackScreen {
			return newApplicationSubmitModel(n.applications, n.documentSvc, n.documents, n.openURL, a, n.exportDir)
		},
	}
	return n
}

func (n *Navigation) Instance() message.ScreenInstance { return n.instance }

func (n *Navigation) Routes() []*model.Route {
	return []*model.Route{
		model.Always(n.resized),
		model.Always(n.exported),
		model.Always(n.hideArchivedChanged),
	}
}

func (n *Navigation) resized(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	n.sized = e.Payload.Value
	return nil
}

// exported remembers where the export went, for the next export or submit
// to start from.
func (n *Navigation) exported(e message.Event[message.Ran[message.ExportReport]]) tea.Cmd {
	n.exportDir = e.Payload.Value.Dir
	return nil
}

func (n *Navigation) hideArchivedChanged(e message.Event[message.Signal[message.HideArchived]]) tea.Cmd {
	n.hideArchived = e.Payload.Value.Hidden
	return nil
}

// NavRoutes are a's routes for each Nav, one per payload, opening what
// its table has.
func (n *Navigation) NavRoutes(a *App) []*model.Route {
	return []*model.Route{
		model.Always(navTo(a, n.none)),
		model.Always(navTo(a, n.company)),
		model.Always(navTo(a, n.filterSelection)),
		model.Always(navTo(a, n.postingDetail)),
		model.Always(navTo(a, n.application)),
		model.Always(navTo(a, n.applicationDetail)),
		model.Always(navTo(a, n.documentReview)),
	}
}
