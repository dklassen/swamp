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
	"github.com/charmbracelet/x/cellbuf"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
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
	// postingReading and scoresReading are the reads for writes elsewhere.
	postingReading, scoresReading reading
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
		model.Always(m.readFailed, model.From(m.instance)),
		model.Always(m.applicationChanged),
		model.Always(m.reviewChanged),
		model.Always(m.postingChanged, model.About(message.TypePosting, m.posting.ID)),
		model.Always(m.scoreChanged, model.About(message.TypePosting, m.posting.ID)),
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
	return tea.Batch(m.loadReviews(), m.postingReading.done(m.loadPosting))
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
	return m.scoresReading.done(m.loadScores)
}

// readFailed ends both reads, since it can't tell which failed.
func (m *postingDetailModel) readFailed(message.Event[message.Error]) tea.Cmd {
	return tea.Batch(m.postingReading.done(m.loadPosting), m.scoresReading.done(m.loadScores))
}

// postingChanged reads the posting again, once at a time for a run of
// writes.
func (m *postingDetailModel) postingChanged(message.Event[message.Changed[message.Posting]]) tea.Cmd {
	return m.postingReading.start(m.loadPosting)
}

// scoreChanged reads the scores again, once at a time for a run of
// writes.
func (m *postingDetailModel) scoreChanged(message.Event[message.Changed[message.Score]]) tea.Cmd {
	return m.scoresReading.start(m.loadScores)
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

func (m *postingDetailModel) crumbs(beneath message.Screen) []string {
	// Application detail has already named it.
	if beneath == message.ScreenApplicationDetail {
		return []string{"Posting"}
	}
	return []string{m.posting.Title}
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

// postingDetailContent renders a posting's fields, application state, and
// description as the scrollable body of the detail view (title is rendered
// separately, as a fixed header outside the viewport). application/
// hasApplication are passed in rather than fetched here so this stays a
// pure function of already-loaded state.
//
// docs resolves the application's document paths and their presence via
// os.Stat -- done inline here rather than through a separate
// tea.Cmd/tea.Msg round trip like the rest of this file's store-backed
// state, since checking whether two local files exist is cheap/local
// enough that a second async fetch just to avoid it here would be
// over-applying that convention. When hasApplication
// is false, no documents section is rendered at all -- "no application
// -> show nothing".
//
// latestReviews is, like application, read by posting detail and passed
// in rather than queried here -- a DB read, unlike docs.Status's local
// os.Stat, follows the same tea.Cmd/tea.Msg convention as the rest of
// this file's store-backed state (#83). A document with no entry in the
// map renders as "not reviewed".
func postingDetailContent(p posting.Posting, application application.Application, hasApplication bool, docs *documents.Store, latestReviews map[document.Type]document.Review, width int) string {
	var b strings.Builder
	b.WriteString(sectionHeading("Posting", width) + "\n")
	fields := []struct{ label, value string }{
		{"Department", p.Department},
		{"Team", p.Team},
		{"Location", p.Location},
		{"Employment type", p.EmploymentType},
		{"Workplace type", p.WorkplaceType},
		{"Status", p.ListingStatus.String()},
		{"Job URL", p.JobURL},
		{"Application URL", p.ApplicationURL},
	}
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		b.WriteString(detailField(f.label, f.value, width))
	}
	b.WriteString("\n" + sectionHeading("Application", width) + "\n")
	if hasApplication {
		b.WriteString(detailField("Status", applicationStatusLabel(application.Status), width))
		if application.Notes != "" {
			b.WriteString(detailField("Notes", application.Notes, width))
		}
		status := docs.Status(application.ID)
		b.WriteString("\n")
		for _, documentType := range document.Types() {
			doc, err := status.Doc(documentType)
			if err != nil {
				continue
			}
			presence := "not found"
			if doc.Exists {
				presence = "found"
			}
			label := documentTitle(documentType)
			// The badge is placed after wrapping rather than wrapped with the path:
			// "[not reviewed]" contains a space, and word wrapping could split it.
			lines := wrapDetailValue(presence+" ("+doc.Path+")", width)
			review, hasReview := latestReviews[documentType]
			badge := reviewBadge(review, hasReview)
			last := len(lines) - 1
			switch valueWidth := detailValueWidth(width); {
			case width > 0 && valueWidth < lipgloss.Width(badge):
				// The value column is narrower than the badge (or there's no room
				// for one at all), so indenting the badge to it would overrun the
				// width and get it wrapped anyway. Give it a line of its own from
				// the left edge instead: out of alignment, but in one piece.
				b.WriteString(detailRow(label, lines) + badge + "\n")
			case valueWidth > 0 && lipgloss.Width(lines[last])+1+lipgloss.Width(badge) > valueWidth:
				b.WriteString(detailRow(label, append(lines, badge)))
			default:
				lines[last] += " " + badge
				b.WriteString(detailRow(label, lines))
			}
			if hasReview && review.Notes != "" {
				b.WriteString(detailField("", model.DimStyle.Render("Notes: "+review.Notes), width))
			}
		}
	} else {
		b.WriteString(model.HelpStyle.Render("No application started -- press 'a' to start one.") + "\n")
	}
	if desc := p.DescriptionText; desc != "" {
		b.WriteString("\n" + sectionHeading("Description", width) + "\n" + desc + "\n")
	}
	return b.String()
}

// sectionHeading renders a posting-detail section heading: name, then a
// dim horizontal rule filling the rest of width, so each section reads as
// its own block. width <= 0 (before the first tea.WindowSizeMsg) gets a
// short fixed rule rather than none.
func sectionHeading(name string, width int) string {
	ruleWidth := 3
	if rest := width - lipgloss.Width(name) - 1; rest > ruleWidth {
		ruleWidth = rest
	}
	return model.SectionStyle.Render(name) + " " + model.DimStyle.Render(strings.Repeat("─", ruleWidth))
}

// detailLabelWidth is the width of posting detail's label column, sized
// to its longest label ("Application URL") plus a gap, so every value in
// the Posting and Application sections starts in the same column.
const detailLabelWidth = len("Application URL") + 2

// detailField renders one posting-detail row: label in a fixed-width
// column, then value word-wrapped to the rest of width, with continuation
// lines indented to the value column instead of falling back under the
// label. width <= 0 (before the first tea.WindowSizeMsg) leaves the value
// unwrapped.
func detailField(label, value string, width int) string {
	return detailRow(label, wrapDetailValue(value, width))
}

// detailValueWidth is the width left for a value after the label column,
// or 0 (unconstrained) when width is unknown or too narrow to fit one.
func detailValueWidth(width int) int {
	return max(width-detailLabelWidth, 0)
}

// wrapDetailValue word-wraps value to the value column's width, as lines.
func wrapDetailValue(value string, width int) []string {
	if valueWidth := detailValueWidth(width); valueWidth > 0 {
		value = cellbuf.Wrap(value, valueWidth, "")
	}
	return strings.Split(value, "\n")
}

// detailRow lays out a label and its already-wrapped value lines: the
// label padded to detailLabelWidth, then the value, with continuation
// lines indented to the value column.
func detailRow(label string, lines []string) string {
	indent := strings.Repeat(" ", detailLabelWidth)
	for i := 1; i < len(lines); i++ {
		lines[i] = indent + lines[i]
	}
	gap := strings.Repeat(" ", detailLabelWidth-lipgloss.Width(label))
	return model.FieldLabel.Render(label) + gap + strings.Join(lines, "\n") + "\n"
}
