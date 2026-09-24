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
	width          int
	posting        store.Posting
	application    store.Application
	hasApplication bool

	// canNavigateSiblings reports whether prev/next posting navigation
	// can actually do anything here: it needs App.postings to contain this
	// posting, which it does when the screen was reached by browsing a
	// company's posting list, but not via application detail's 'p' fast
	// path. The keys themselves no-op safely either way; this exists so
	// the help line stops advertising a move that silently does nothing
	// (see issue #93).
	canNavigateSiblings bool

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
func newPostingDetailModel(s *store.Store, docs *documents.Store, width, height int, p store.Posting, app store.Application, hasApp bool, latestReviews map[store.DocumentType]store.DocumentReview, canNavigateSiblings bool) postingDetailModel {
	inner := detailInnerWidth(width)
	vp := viewport.New(width, height)
	vp.SetContent(indentLines(wrapToWidth(postingDetailContent(p, app, hasApp, docs, latestReviews, inner), inner), detailPadding))
	return postingDetailModel{store: s, documents: docs, viewport: vp, width: width, posting: p, application: app, hasApplication: hasApp, latestReviews: latestReviews, canNavigateSiblings: canNavigateSiblings}
}

// detailPadding is the margin (columns) kept clear on each side of the
// title and body, so text doesn't sit hard against the terminal edges.
const detailPadding = 2

// detailInnerWidth is the width left for text once detailPadding is taken
// off both sides. width <= 0 (before the first tea.WindowSizeMsg) stays
// unconstrained, as wrapToWidth expects; a terminal too narrow for the
// margins gets the full width rather than a nonsensical one.
func detailInnerWidth(width int) int {
	if width <= 2*detailPadding {
		return width
	}
	return width - 2*detailPadding
}

// indentLines prefixes every non-empty line of s with n spaces. Empty
// lines stay empty rather than becoming trailing whitespace.
func indentLines(s string, n int) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
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

// refreshPostingDetailMsg signals that App should reload this posting's
// application, document status, and reviews from disk/the store without
// leaving posting detail -- e.g. after an external agent (see
// .agents/skills/apply-to-posting) revises a document on disk while the
// user is still looking at this screen (see decisions.log).
type refreshPostingDetailMsg struct{}

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
	case msg.String() == "u":
		return nil, refreshPostingDetailMsg{}
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd, nil
	}
	return nil, nil
}

func (m *postingDetailModel) View() string {
	var b strings.Builder
	// Truncated rather than wrapped: the title sits outside the viewport,
	// and a second title line would push the help line off the screen.
	title := m.posting.Title
	if inner := detailInnerWidth(m.width); inner > 0 {
		title = truncateCol(title, inner)
	}
	b.WriteString(indentLines(titleStyle.Render(title), detailPadding) + "\n")
	b.WriteString(m.viewport.View() + "\n")
	help := "↑/↓ (j/k): scroll  "
	if m.canNavigateSiblings {
		help += "←/→ (h/l): prev/next posting  "
	}
	help += "o: open in browser  a: start application  s: set status  n: edit notes  r: review document  u: refresh  esc/b: back"
	b.WriteString(helpStyle.Render(help))
	return b.String()
}

// resize rebuilds the viewport at new dimensions, keeping the same
// posting/application data -- called on window resize while this screen
// is active (see tea.WindowSizeMsg in App.Update).
func (m *postingDetailModel) resize(width, height int) {
	*m = newPostingDetailModel(m.store, m.documents, width, height, m.posting, m.application, m.hasApplication, m.latestReviews, m.canNavigateSiblings)
}
