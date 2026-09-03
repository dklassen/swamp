package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// postingDetailModel drives the posting-detail screen for a single
// posting. It holds the store/documents it needs for its own commands,
// the posting and application data it was constructed with (a snapshot,
// not a live reference into App.applicationsByPosting -- App re-seeds
// this model whenever that data changes, see App.Update), and its own
// private viewport. No other screen reads or writes this state, and
// this model never reaches into App's postings slice or cursor.
type postingDetailModel struct {
	store          *store.Store
	documents      *documents.Store
	viewport       viewport.Model
	posting        store.Posting
	application    store.Application
	hasApplication bool

	// latestReviews holds the most recent DocumentReview per document
	// type (keyed by store.DocumentTypeCoverLetter/DocumentTypeResume),
	// loaded async via loadDocumentReviews the same way application
	// itself is (see decisions.log #83) -- absent when not yet loaded,
	// or nil when this posting has no application.
	latestReviews map[store.DocumentType]store.DocumentReview
}

// newPostingDetailModel returns a detail screen for p, sized to
// width/height and scrolled to the top -- called both when first
// entering the screen and whenever the underlying posting/application
// data changes (navigating to a different posting, an application being
// created/updated, or a window resize), matching the pre-extraction
// showPostingDetail's "always rebuild, always reset scroll" behavior.
func newPostingDetailModel(s *store.Store, docs *documents.Store, width, height int, p store.Posting, app store.Application, hasApp bool, latestReviews map[store.DocumentType]store.DocumentReview) postingDetailModel {
	vp := viewport.New(width, height)
	vp.SetContent(wrapToWidth(postingDetailContent(p, app, hasApp, docs, latestReviews), width))
	return postingDetailModel{store: s, documents: docs, viewport: vp, posting: p, application: app, hasApplication: hasApp, latestReviews: latestReviews}
}

// backToPostingListMsg signals that App should switch to the
// posting-list screen.
type backToPostingListMsg struct{}

// navigatePostingMsg signals that App should move to the posting
// direction steps away from postingID in App's postings slice (1 for
// next/l, -1 for prev/h) -- this model has no access to that slice
// itself, only the one posting it's currently showing.
type navigatePostingMsg struct {
	postingID int64
	direction int
}

// enterApplicationStatusMsg signals that App should switch to the
// application-status-select screen, seeded from this application's
// current status.
type enterApplicationStatusMsg struct {
	postingID     int64
	currentStatus store.ApplicationStatus
}

// enterApplicationNotesMsg signals that App should switch to the
// application-notes-edit screen, seeded from this application's current
// notes.
type enterApplicationNotesMsg struct {
	postingID    int64
	currentNotes string
}

// enterDocumentReviewSelectMsg signals that App should switch to the
// document-review-select screen for this application.
type enterDocumentReviewSelectMsg struct {
	applicationID int64
}

func (m *postingDetailModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyRight, msg.String() == "l":
		return nil, navigatePostingMsg{postingID: m.posting.ID, direction: 1}
	case msg.Type == tea.KeyLeft, msg.String() == "h":
		return nil, navigatePostingMsg{postingID: m.posting.ID, direction: -1}
	case msg.Type == tea.KeyEsc, msg.String() == "b":
		return nil, backToPostingListMsg{}
	case msg.String() == "o":
		if m.posting.JobURL != "" {
			return openInBrowser(m.posting.JobURL), nil
		}
	case msg.String() == "a":
		if !m.hasApplication {
			return createApplication(m.store, m.posting.ID), nil
		}
	case msg.String() == "s":
		if m.hasApplication {
			return nil, enterApplicationStatusMsg{postingID: m.posting.ID, currentStatus: m.application.Status}
		}
	case msg.String() == "n":
		if m.hasApplication {
			return nil, enterApplicationNotesMsg{postingID: m.posting.ID, currentNotes: m.application.Notes}
		}
	case msg.String() == "r":
		if m.hasApplication {
			return nil, enterDocumentReviewSelectMsg{applicationID: m.application.ID}
		}
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd, nil
	}
	return nil, nil
}

func (m *postingDetailModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.posting.Title) + "\n")
	b.WriteString(m.viewport.View() + "\n")
	b.WriteString(helpStyle.Render("↑/↓ (j/k): scroll  ←/→ (h/l): prev/next posting  o: open in browser  a: start application  s: set status  n: edit notes  r: review document  esc/b: back"))
	return b.String()
}

// resize rebuilds the viewport at new dimensions, keeping the same
// posting/application data -- called on window resize while this screen
// is active (see tea.WindowSizeMsg in App.Update).
func (m *postingDetailModel) resize(width, height int) {
	*m = newPostingDetailModel(m.store, m.documents, width, height, m.posting, m.application, m.hasApplication, m.latestReviews)
}
