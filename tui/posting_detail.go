package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// postingDetailModel is one posting, its application and that
// application's reviews, and the posting's scores, which it loads and
// keeps.
type postingDetailModel struct {
	postingService  *service.PostingService
	documentService *service.DocumentService
	scoringService  *service.ScoringService
	documents       *documents.Store

	posting message.Posting
	// application is zero while the posting has none.
	application message.Application
	reviews     message.ReviewList
	scores      []message.Score
	// siblings is the postings previous and next step through, by ID in
	// list order; empty when opened outside the posting list.
	siblings []int64

	viewport viewport.Model
	width    int
	help     string
	focused  bool
	instance message.ScreenInstance
}

func newPostingDetailModel(postings *service.PostingService, documentService *service.DocumentService, scores *service.ScoringService, docs *documents.Store, d message.PostingDetail, size message.ContentSize) *postingDetailModel {
	m := &postingDetailModel{
		postingService:  postings,
		documentService: documentService,
		scoringService:  scores,
		documents:       docs,
		posting:         d.Posting,
		application:     d.Application,
		siblings:        d.Siblings,
		viewport:        viewport.New(),
		instance:        message.NewScreenInstance(),
	}
	m.setSize(size)
	return m
}

func (m *postingDetailModel) Instance() message.ScreenInstance { return m.instance }

func (m *postingDetailModel) Focused() bool { return m.focused }

func (m *postingDetailModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.postingRead, model.From(m.instance)),
		model.Always(m.reviewsRead, model.From(m.instance)),
		model.Always(m.scoresRead, model.From(m.instance)),
		model.Always(m.postingLoadRequested),
		model.Always(m.scoresLoadRequested),
		model.Always(m.applicationChanged),
		model.Always(m.reviewChanged),
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

// Init reads the posting and its application, whose reviews follow, and
// its scores.
func (m *postingDetailModel) Init() tea.Cmd {
	return tea.Batch(m.loadPosting(), m.loadScores())
}

// Update has nothing to do: everything the detail handles is registered.
func (m *postingDetailModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *postingDetailModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *postingDetailModel) hasApplication() bool { return m.application.ID != 0 }

func (m *postingDetailModel) loadPosting() tea.Cmd {
	return loadPostingApplication(m.postingService, m.src(), m.posting.ID)
}

func (m *postingDetailModel) loadReviews() tea.Cmd {
	if !m.hasApplication() {
		return nil
	}
	return loadReviewList(m.documentService, m.documents, m.src(), m.application.ID)
}

func (m *postingDetailModel) loadScores() tea.Cmd {
	return loadPostingScores(m.scoringService, m.src(), m.posting.ID)
}

// postingRead reloads the reviews, since the application's documents may
// have changed with it.
func (m *postingDetailModel) postingRead(e message.Event[message.Read[message.PostingApplication]]) tea.Cmd {
	if e.Payload.Value.Application.ID != m.application.ID {
		m.reviews = message.ReviewList{}
	}
	m.posting, m.application = e.Payload.Value.Posting, e.Payload.Value.Application
	m.setContent()
	return m.loadReviews()
}

// reviewsRead drops a read for an application this posting no longer has.
func (m *postingDetailModel) reviewsRead(e message.Event[message.Read[message.ReviewList]]) tea.Cmd {
	if e.Meta.Subject.ID != m.application.ID {
		return nil
	}
	m.reviews = e.Payload.Value
	m.setContent()
	return nil
}

func (m *postingDetailModel) scoresRead(e message.Event[message.Read[message.PostingScores]]) tea.Cmd {
	m.scores = e.Payload.Value.Scores
	m.setContent()
	return nil
}

func (m *postingDetailModel) postingLoadRequested(message.Event[message.Load[message.PostingApplication]]) tea.Cmd {
	return m.loadPosting()
}

func (m *postingDetailModel) scoresLoadRequested(message.Event[message.Load[message.PostingScores]]) tea.Cmd {
	return m.loadScores()
}

// applicationChanged takes a write to its posting's application, whoever
// made it. A new application is about an ID it doesn't know yet, so it
// goes by the posting.
func (m *postingDetailModel) applicationChanged(e message.Event[message.Changed[message.Application]]) tea.Cmd {
	if e.Payload.Before.PostingID != m.posting.ID && e.Payload.After.PostingID != m.posting.ID {
		return nil
	}
	m.application = e.Payload.After
	if e.Meta.Op == message.OpDeleted {
		m.reviews = message.ReviewList{}
	}
	m.setContent()
	return nil
}

// reviewChanged takes a review of its application's document, whoever
// saved it. Its application can change while it's open, so it isn't
// filtered by About.
func (m *postingDetailModel) reviewChanged(e message.Event[message.Changed[message.Review]]) tea.Cmd {
	if !m.hasApplication() || e.Meta.Subject.ID != m.application.ID {
		return nil
	}
	m.reviews.Reviews = withReview(m.reviews.Reviews, e.Payload.After)
	m.setContent()
	return nil
}

func (m *postingDetailModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.setSize(e.Payload.Value)
	return nil
}

func (m *postingDetailModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

func (m *postingDetailModel) keyPressed(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "right", "l":
		return m.step(1)
	case "left", "h":
		return m.step(-1)
	case "esc", "b":
		return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), ""))
	case "o":
		if m.posting.JobURL != "" {
			return openInBrowser(m.src(), message.Subject{Type: message.TypePosting, ID: m.posting.ID}, m.posting.JobURL)
		}
	case "a":
		if !m.hasApplication() {
			return createApplication(m.postingService, m.src(), m.posting.ID)
		}
	case "s":
		if m.hasApplication() {
			return m.openApplication(message.ScreenApplicationStatus)
		}
	case "n":
		if m.hasApplication() {
			return m.openApplication(message.ScreenApplicationNotes)
		}
	case "r":
		if m.hasApplication() {
			return m.openApplication(message.ScreenDocumentReviewSelect)
		}
	case "u":
		return tea.Batch(m.loadPosting(), m.loadScores())
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(key)
		return cmd
	}
	return nil
}

// step replaces the detail with the sibling delta away, which reads its
// own posting and application.
func (m *postingDetailModel) step(delta int) tea.Cmd {
	i := slices.Index(m.siblings, m.posting.ID)
	next := i + delta
	if i < 0 || next < 0 || next >= len(m.siblings) {
		return nil
	}
	id := m.siblings[next]
	return message.Replace(m.src(), message.ScreenPostingDetail, message.Subject{Type: message.TypePosting, ID: id},
		message.PostingDetail{Posting: message.Posting{ID: id}, Siblings: m.siblings})
}

func (m *postingDetailModel) openApplication(screen message.Screen) tea.Cmd {
	return message.Push(m.src(), screen, message.Subject{Type: message.TypeApplication, ID: m.application.ID}, m.application)
}

// canStep is whether previous and next can move anywhere.
func (m *postingDetailModel) canStep() bool {
	return slices.Contains(m.siblings, m.posting.ID)
}

// setSize lays the detail out again at size, since the viewport truncates
// rather than rewraps content set at another width.
func (m *postingDetailModel) setSize(size message.ContentSize) {
	m.width = size.Width
	m.viewport.SetWidth(size.Width)
	m.help = indentLines(wrapToWidth(postingDetailHelp(m.canStep()), detailInnerWidth(m.width)), detailPadding)
	m.setContent()
	m.setHeight(size.Rows)
}

// setContent lays the detail out at the model's width, keeping the scroll.
func (m *postingDetailModel) setContent() {
	inner := detailInnerWidth(m.width)
	offset := m.viewport.YOffset()
	body := postingScoresSection(m.posting, m.scores, inner) +
		postingDetailContent(postingOf(m.posting), applicationOf(m.application), m.hasApplication(), m.documents, reviewsOf(m.reviews), inner)
	m.viewport.SetContent(indentLines(wrapToWidth(body, inner), detailPadding))
	m.viewport.SetYOffset(offset)
}

// postingScoresSection is the posting's fit: its current score with its
// reason and what gave it, then the earlier ones. Empty when it has none.
func postingScoresSection(p message.Posting, scores []message.Score, width int) string {
	if len(scores) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(sectionHeading("Fit", width) + "\n")
	current := scores[0]
	b.WriteString(detailField("Score", fmt.Sprintf("%d · %s", current.Score, current.Reason), width))
	b.WriteString(detailField("Scored", scoredBy(current), width))
	if current.ContentSHA256 != p.ContentSHA256 {
		b.WriteString(model.HelpStyle.Render("The posting has changed since this score.") + "\n")
	}
	if len(scores) > 1 {
		b.WriteString(model.FieldLabel.Render("Earlier") + "\n")
		for _, earlier := range scores[1:] {
			b.WriteString(model.DimStyle.Render(fmt.Sprintf("  %d · %s (%s)", earlier.Score, earlier.Reason, scoredBy(earlier))) + "\n")
		}
	}
	return b.String() + "\n"
}

// scoredBy says when a score was given and by what.
func scoredBy(s message.Score) string {
	by := s.ScoredAt.Local().Format("Jan 2") + " by " + s.Model
	if s.ClientName != "" {
		by += " (" + strings.TrimSpace(s.ClientName+" "+s.ClientVersion) + ")"
	}
	return by
}

// setHeight fits the screen into height terminal rows -- everything App
// leaves it below its header and status/error banner (see App.screenRows)
// -- by giving the viewport whatever the help line doesn't take. It's
// measured as rendered, margin and wrapped lines included, so the screen
// can't outgrow the terminal and push the header off the top. The scroll
// position is kept (clamped to the new height), since the banner can come
// and go while the user is reading.
func (m *postingDetailModel) setHeight(height int) {
	chrome := lipgloss.Height(model.HelpStyle.Render(m.help))
	m.viewport.SetHeight(max(height-chrome, 0))
	m.viewport.SetYOffset(m.viewport.YOffset())
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

func (m *postingDetailModel) View() tea.View {
	return tea.NewView(m.viewport.View() + "\n" + model.HelpStyle.Render(m.help))
}

// postingDetailHelp is the detail screen's key help, advertising prev/next
// navigation only when it can actually do something (#93).
func postingDetailHelp(canStep bool) string {
	help := "↑/↓ (j/k): scroll  "
	if canStep {
		help += "←/→ (h/l): prev/next posting  "
	}
	return help + "o: open in browser  a: start application  s: set status  n: edit notes  r: review document  u: refresh  esc/b: back"
}

// loadPostingApplication reads a posting and its application, if it has
// one.
func loadPostingApplication(postings *service.PostingService, src message.Source, postingID int64) tea.Cmd {
	subject := message.Subject{Type: message.TypePosting, ID: postingID}
	return func() tea.Msg {
		ctx := context.Background()
		p, err := postings.Get(ctx, postingID)
		if err != nil {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		read := message.PostingApplication{Posting: postingSnapshot(*p)}
		app, err := postings.Application(ctx, postingID)
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		default:
			read.Application = applicationSnapshot(*app)
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.PostingApplication]{Value: read})
	}
}

// loadReviewList reads the latest review of each of an application's
// documents that still describes it.
func loadReviewList(documentService *service.DocumentService, docs *documents.Store, src message.Source, applicationID int64) tea.Cmd {
	subject := message.Subject{Type: message.TypeApplication, ID: applicationID}
	return func() tea.Msg {
		reviews, err := currentReviews(documentService, docs, applicationID)
		if err != nil {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		var list message.ReviewList
		for _, r := range reviews {
			list.Reviews = append(list.Reviews, reviewSnapshot(r))
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.ReviewList]{Value: list})
	}
}

func loadPostingScores(scores *service.ScoringService, src message.Source, postingID int64) tea.Cmd {
	subject := message.Subject{Type: message.TypePosting, ID: postingID}
	return func() tea.Msg {
		postingScores, err := scores.ForPosting(context.Background(), postingID)
		if err != nil {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		var read message.PostingScores
		for _, s := range postingScores {
			read.Scores = append(read.Scores, scoreSnapshot(s))
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.PostingScores]{Value: read})
	}
}
