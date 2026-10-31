package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// Screens is every screen there is: the tabs, and what each Nav opens,
// built from the services it's built with and the session's state, which
// it keeps from the messages that carry it. Posting detail giving way to
// the next posting, and the review form taking the picker's place, are
// the replaces meant.
type Screens struct {
	instance message.ScreenInstance

	activeApplicationList *activeApplicationListModel
	companyList           *companyListModel
	allPostings           *allPostingsModel

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

// defaultExportDir is where the export screen points before anything has
// been exported this session. Configured here rather than via the
// environment or a flag: it's a starting point the user edits in the
// prompt whenever they want elsewhere, so a code-level default is the
// whole configuration surface it needs. A leading "~" is expanded when
// the export actually runs (see expandPath).
const defaultExportDir = "~/Desktop"

func NewScreens(
	companies *service.CompanyService,
	postings *service.PostingService,
	applications *service.ApplicationService,
	documentService *service.DocumentService,
	scores *service.ScoringService,
	syncer *sync.Syncer,
	docs *documents.Store,
) *Screens {
	return &Screens{
		instance:              message.NewScreenInstance(),
		activeApplicationList: newActiveApplicationListModel(applications, documentService, docs),
		companyList:           newCompanyListModel(companies, postings),
		allPostings:           newAllPostingsModel(postings),
		companies:             companies,
		postings:              postings,
		applications:          applications,
		documentSvc:           documentService,
		scores:                scores,
		syncer:                syncer,
		documents:             docs,
		openURL:               openInBrowser,
		exportDir:             defaultExportDir,
		hideArchived:          true,
	}
}

// Tabs are the screens always open, in the order tab goes through them.
func (s *Screens) Tabs() []Tab {
	return []Tab{
		{Kind: message.ScreenActiveApplications, Screen: s.activeApplicationList},
		{Kind: message.ScreenCompanyList, Screen: s.companyList},
		{Kind: message.ScreenAllPostings, Screen: s.allPostings},
	}
}

// Tables are what each Nav payload opens.
func (s *Screens) Tables() []Table {
	return []Table{
		Opens(map[message.Screen]func(message.None) stackScreen{
			message.ScreenCompanyForm: func(message.None) stackScreen {
				return newCompanyFormModel(s.syncer)
			},
		}),
		Opens(map[message.Screen]func(message.Company) stackScreen{
			message.ScreenCompanyEdit: func(c message.Company) stackScreen {
				return newCompanyEditModel(s.companies, c.ID, c.Name)
			},
			message.ScreenCompanyPostings: func(c message.Company) stackScreen {
				return newPostingListModel(s.companies, s.postings, c, s.hideArchived, s.sized)
			},
		}),
		Opens(map[message.Screen]func(message.FilterSelection) stackScreen{
			message.ScreenFilterSelect: func(f message.FilterSelection) stackScreen {
				return newFilterSelectModel(s.companies, f, s.sized)
			},
		}),
		Opens(map[message.Screen]func(message.PostingDetail) stackScreen{
			message.ScreenPostingDetail: func(p message.PostingDetail) stackScreen {
				return newPostingDetailModel(s.postings, s.documentSvc, s.scores, s.documents, p, s.sized)
			},
		}),
		Opens(map[message.Screen]func(message.Application) stackScreen{
			message.ScreenApplicationStatus: func(a message.Application) stackScreen {
				return newApplicationStatusModel(s.applications, a)
			},
			message.ScreenApplicationNotes: func(a message.Application) stackScreen {
				return newApplicationNotesModel(s.applications, a, s.sized)
			},
			message.ScreenDocumentReviewSelect: func(a message.Application) stackScreen {
				return newDocumentReviewSelectModel(s.documents, a)
			},
			message.ScreenApplicationForm: func(a message.Application) stackScreen {
				return newApplicationFormModel(s.postings, a, s.sized)
			},
		}),
		Opens(map[message.Screen]func(message.DocumentReview) stackScreen{
			message.ScreenDocumentReviewForm: func(r message.DocumentReview) stackScreen {
				return newDocumentReviewFormModel(s.documentSvc, s.documents, r, s.sized)
			},
		}),
		Opens(map[message.Screen]func(message.ApplicationDetail) stackScreen{
			message.ScreenApplicationDetail: func(a message.ApplicationDetail) stackScreen {
				return newApplicationDetailModel(s.applications, s.postings, s.companies, s.documentSvc, s.documents, a, s.sized.Width)
			},
			message.ScreenApplicationExport: func(a message.ApplicationDetail) stackScreen {
				return newApplicationExportModel(s.documentSvc, s.documents, a, s.exportDir, s.sized)
			},
			message.ScreenApplicationSubmit: func(a message.ApplicationDetail) stackScreen {
				return newApplicationSubmitModel(s.applications, s.documentSvc, s.documents, s.openURL, a, s.exportDir)
			},
		}),
	}
}

func (s *Screens) Instance() message.ScreenInstance { return s.instance }

// Routes keep the session state screens open with.
func (s *Screens) Routes() []*model.Route {
	return []*model.Route{
		model.Always(s.resized),
		model.Always(s.exported),
		model.Always(s.hideArchivedChanged),
	}
}

func (s *Screens) resized(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	s.sized = e.Payload.Value
	return nil
}

// exported remembers where the export went, for the next export or submit
// to start from.
func (s *Screens) exported(e message.Event[message.Ran[message.ExportReport]]) tea.Cmd {
	s.exportDir = e.Payload.Value.Dir
	return nil
}

func (s *Screens) hideArchivedChanged(e message.Event[message.Signal[message.HideArchived]]) tea.Cmd {
	s.hideArchived = e.Payload.Value.Hidden
	return nil
}
