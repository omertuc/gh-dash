package prview

import (
	"fmt"
	"image/color"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/carousel"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/cmpcontroller"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/fuzzyselect"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/inputbox"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

var (
	htmlCommentRegex = regexp.MustCompile("(?U)<!--(.|[[:space:]])*-->")
	lineCleanupRegex = regexp.MustCompile(`((\n)+|^)([^\r\n]*\|[^\r\n]*(\n)?)+`)
	foldBodyHeight   = 8
)

type Model struct {
	ctx             *context.ProgramContext
	sectionId       int
	pr              *prrow.PullRequest
	width           int
	carousel        carousel.Model
	tabsHint        string
	activityCache   *activityCache
	editor          cmpcontroller.Controller
	summaryViewMore bool
	// expandedCommit is the oid of the commit shown with its full message,
	// e.g. the focused one
	expandedCommit string
	// expandedCheck identifies the check shown with its details, e.g. the
	// focused one
	expandedCheck string
	// commitFiles narrows the files tab to a commit's files, or is nil to show
	// all of the PR's files
	commitFiles *commitFiles
}

// commitFiles are the files changed by a commit, shown in the files tab.
type commitFiles struct {
	oid            string
	abbreviatedOid string
	files          []data.ChangedFile
	loading        bool
	err            error
}

// CommitFilesMsg carries the files changed by a commit, once fetched.
type CommitFilesMsg struct {
	Oid   string
	Files []data.ChangedFile
	Err   error
}

var tabs = []string{" Overview", " Activity", " Commits", " Checks", " Files Changed"}

const (
	overviewTab = iota
	activityTab
	commitsTab
	checksTab
	filesTab
)

func NewModel(ctx *context.ProgramContext) Model {
	c := carousel.NewModel(
		carousel.WithItems(tabs),
		carousel.WithWidth(ctx.MainContentWidth),
	)

	ta := inputbox.DefaultTextArea(ctx)
	cmp := cmpcontroller.New(ctx, inputbox.ModelOpts{TextArea: &ta})

	return Model{
		ctx:           ctx,
		pr:            nil,
		carousel:      c,
		editor:        cmp,
		activityCache: &activityCache{},
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	cmd, handled := m.editor.Update(msg)

	if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "ctrl+d" {
		value := m.editor.Value()
		mode := m.editor.Mode()
		m.editor.Exit()
		if m.pr == nil {
			return m, nil
		}

		sid := tasks.SectionIdentifier{Id: m.sectionId, Type: prssection.SectionType}

		switch mode {
		case cmpcontroller.ModeComment:
			if len(strings.TrimSpace(value)) != 0 {
				return m, tasks.CommentOnPR(m.ctx, sid, m.pr.Data.Primary, value)
			}
			return m, nil

		case cmpcontroller.ModeApprove:
			comment := ""
			if len(strings.TrimSpace(value)) != 0 {
				comment = value
			}
			return m, tasks.ApprovePR(m.ctx, sid, m.pr.Data.Primary, comment)

		case cmpcontroller.ModeAssign:
			usernames := fuzzyselect.AllWords(value)
			if len(usernames) > 0 {
				return m, tasks.AssignPR(m.ctx, sid, m.pr.Data.Primary, usernames)
			}
			return m, nil

		case cmpcontroller.ModeUnassign:
			usernames := fuzzyselect.AllWords(value)
			if len(usernames) > 0 {
				return m, tasks.UnassignPR(m.ctx, sid, m.pr.Data.Primary, usernames)
			}
			return m, nil

		case cmpcontroller.ModeLabel:
			labels := fuzzyselect.CurrentLabels(value)
			if len(labels) > 0 || len(m.pr.Data.Primary.Labels.Nodes) > 0 {
				return m, m.label(labels)
			}
			return m, nil
		}
	}

	if handled {
		return m, cmd
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, keys.PRKeys.PrevSidebarTab):
			m.PrevTab()
		case key.Matches(keyMsg, keys.PRKeys.NextSidebarTab):
			m.NextTab()
		}
	}

	return m, cmd
}

func (m Model) View() string {
	if !m.hasData() {
		return ""
	}

	return lipgloss.JoinVertical(lipgloss.Left, m.ViewHeader(), m.ViewBody())
}

// ViewBody renders the selected tab's content, without the header.
func (m Model) ViewBody() string {
	body, _ := m.ViewBodyWithAnchors()
	return body
}

// ViewBodyWithAnchors renders the selected tab's content along with where each
// comment starts, when the tab shows comments.
func (m Model) ViewBodyWithAnchors() (string, []common.CommentAnchor) {
	if !m.hasData() {
		return "", nil
	}

	if m.carousel.Cursor() == activityTab {
		// Cached, since it re-renders on every keystroke while writing a
		// comment in the editor docked below it
		return m.cachedActivity()
	}

	body := strings.Builder{}
	var anchors []common.CommentAnchor
	switch m.carousel.Cursor() {
	case overviewTab:
		body.WriteString(m.viewOverviewTab())
	case commitsTab:
		var commits string
		commits, anchors = m.renderCommits()
		body.WriteString(commits)
	case checksTab:
		overview := m.renderChecksOverview()
		checks, checkAnchors := m.renderChecks()
		body.WriteString(overview)
		body.WriteString("\n\n")
		body.WriteString(checks)
		// The checks start below the overview and the blank line after it
		for _, a := range checkAnchors {
			a.Line += lipgloss.Height(overview) + 1
			anchors = append(anchors, a)
		}
	case filesTab:
		body.WriteString(m.renderChangedFiles())
	}

	return lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding).
		Render(body.String()), anchors
}

// ViewEditor renders the editor, docked below the preview's content, or ""
// when it isn't open. A detached draft is shown compactly with detachedHint.
func (m Model) ViewEditor(detachedHint string) string {
	if !m.editor.Active() {
		return ""
	}
	editor := m.editor.View()
	if m.editor.Detached() {
		editor = m.editor.DetachedView(detachedHint)
	}
	return lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding).
		Render(m.ctx.Styles.Sidebar.InputBox.Render(editor))
}

// HasDetachedDraft reports whether there's a comment being written that was
// detached from to read the preview.
func (m *Model) HasDetachedDraft() bool {
	return m.editor.Detached() && m.editor.Mode() == cmpcontroller.ModeComment
}

// AttachDraft returns to a detached draft.
func (m *Model) AttachDraft() tea.Cmd {
	return m.editor.Attach()
}

// DetachDraft leaves the comment being written, keeping it as a draft.
func (m *Model) DetachDraft() {
	if m.editor.Mode() == cmpcontroller.ModeComment {
		m.editor.Detach()
	}
}

// DraftValue returns the comment being written, if any.
func (m *Model) DraftValue() string {
	if m.editor.Mode() != cmpcontroller.ModeComment {
		return ""
	}
	return m.editor.Value()
}

// AppendToDraft adds text to the end of the comment being written, on its own
// paragraph.
func (m *Model) AppendToDraft(text string) {
	m.editor.SetValue(common.AppendParagraph(m.editor.Value(), text))
}

// DiscardEditor closes the editor, dropping anything written in it.
func (m *Model) DiscardEditor() {
	if m.editor.Active() {
		m.editor.Exit()
	}
}

type activityCacheKey struct {
	data       *prrow.Data
	isEnriched bool
	width      int
	// The first element of each list, which changes when a list is
	// replaced, e.g. when the PR is fetched again
	comments    any
	numComments int
	reviews     any
	numReviews  int
	threads     any
	numThreads  int
	styles      *context.Styles
	// Adaptive colors and the markdown style depend on it
	hasDarkBackground bool
	minute            time.Time // so times like "3m ago" stay current
}

// activityCache holds the last rendered activity tab. It's a pointer so
// View, which has a value receiver, can fill it.
type activityCache struct {
	key     activityCacheKey
	view    string
	anchors []common.CommentAnchor
}

// cachedActivity renders the activity tab with its padding, reusing the last
// render when nothing it shows has changed. The tab re-renders on every
// keystroke while typing a reply, and rendering many comments is slow.
func (m Model) cachedActivity() (string, []common.CommentAnchor) {
	if m.activityCache == nil {
		view, anchors := m.renderActivityWithAnchors()
		return lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding).Render(view), anchors
	}
	enriched := m.pr.Data.Enriched
	key := activityCacheKey{
		data:        m.pr.Data,
		isEnriched:  m.pr.Data.IsEnriched,
		width:       m.width,
		numComments: len(enriched.Comments.Nodes),
		numReviews:  len(enriched.Reviews.Nodes),
		numThreads:  len(enriched.ReviewThreads.Nodes),
		styles:      &m.ctx.Styles,
		minute:      time.Now().Truncate(time.Minute),

		hasDarkBackground: m.ctx.HasDarkBackground,
	}
	if len(enriched.Comments.Nodes) > 0 {
		key.comments = &enriched.Comments.Nodes[0]
	}
	if len(enriched.Reviews.Nodes) > 0 {
		key.reviews = &enriched.Reviews.Nodes[0]
	}
	if len(enriched.ReviewThreads.Nodes) > 0 {
		key.threads = &enriched.ReviewThreads.Nodes[0]
	}
	if m.activityCache.key == key && m.activityCache.view != "" {
		return m.activityCache.view, slices.Clone(m.activityCache.anchors)
	}
	view, anchors := m.renderActivityWithAnchors()
	view = lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding).Render(view)
	*m.activityCache = activityCache{key: key, view: view, anchors: slices.Clone(anchors)}
	return view, anchors
}

// ViewHeader renders the part of the preview above the selected tab's
// content: the PR's name, title, branches, author and the tab bar.
func (m Model) ViewHeader() string {
	if !m.hasData() {
		return ""
	}
	return m.viewHeader()
}

// renderTabsHint renders a hint for the keys that switch tabs, e.g. "]→ [←",
// shown at the right end of the tab bar.
func (m *Model) renderTabsHint() string {
	return lipgloss.NewStyle().
		Foreground(m.ctx.Theme.FaintText).
		Render(keys.HintKeys(keys.PRKeys.NextSidebarTab) + "→ " +
			keys.HintKeys(keys.PRKeys.PrevSidebarTab) + "← ")
}

func (m *Model) viewHeader() string {
	header := strings.Builder{}

	header.WriteString(m.renderFullNameAndNumber())
	header.WriteString("\n")

	header.WriteString(m.renderTitle())
	header.WriteString("\n\n")
	header.WriteString(m.renderBranches())
	header.WriteString("\n\n")
	header.WriteString(m.renderAuthor())
	header.WriteString("\n\n")
	header.WriteString(lipgloss.NewStyle().Width(m.width).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Render(lipgloss.JoinHorizontal(lipgloss.Bottom, m.carousel.View(), m.tabsHint)),
	)

	header.WriteString("\n")
	return header.String()
}

func (m *Model) viewOverviewTab() string {
	body := strings.Builder{}
	reviewers := m.renderRequestedReviewers()
	if reviewers != "" {
		body.WriteString(reviewers)
		body.WriteString("\n\n")
	}

	labels := m.renderLabels()
	if labels != "" {
		body.WriteString(labels)
		body.WriteString("\n\n")
	}

	body.WriteString(m.renderSummary())
	body.WriteString("\n\n")
	body.WriteString(
		m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(" Changes"),
	)
	body.WriteString("\n")
	body.WriteString(m.renderChangesOverview())
	body.WriteString("\n\n")
	body.WriteString(
		m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(" Checks"),
	)
	body.WriteString("\n")
	body.WriteString(m.renderChecksOverview())

	return body.String()
}

func (m *Model) ViewCompletions() string {
	if !m.hasData() {
		return ""
	}
	return m.editor.ViewCompletions()
}

func (m *Model) InputBoxLineFromBottom() int {
	return m.editor.LineFromBottom()
}

func (m *Model) renderFullNameAndNumber() string {
	if !m.hasData() {
		return ""
	}

	return common.RenderPreviewHeader(
		m.ctx.Theme,
		m.width,
		fmt.Sprintf(
			"%s · #%d",
			m.pr.Data.Primary.GetRepoNameWithOwner(),
			m.pr.Data.Primary.GetNumber(),
		),
	)
}

func (m *Model) renderTitle() string {
	if !m.hasData() {
		return ""
	}

	return common.RenderPreviewTitle(
		m.ctx.Theme,
		m.ctx.Styles.Common,
		m.width,
		m.pr.Data.Primary.Title,
	)
}

func (m *Model) renderBranches() string {
	return lipgloss.JoinHorizontal(lipgloss.Left,
		" ",
		m.renderStatusPill(),
		" ",
		lipgloss.NewStyle().
			Foreground(m.ctx.Theme.SecondaryText).
			Render(m.pr.Data.Primary.BaseRefName+"  "+m.pr.Data.Primary.HeadRefName))
}

func (m *Model) renderStatusPill() string {
	var bgColor color.Color
	switch m.pr.Data.Primary.State {
	case "OPEN":
		if m.pr.Data.Primary.IsDraft {
			bgColor = m.ctx.Theme.FaintText.Dark
		} else {
			bgColor = m.ctx.Styles.Colors.OpenPR.Dark
		}
	case "CLOSED":
		bgColor = m.ctx.Styles.Colors.ClosedPR.Dark
	case "MERGED":
		bgColor = m.ctx.Styles.Colors.MergedPR.Dark
	}

	return m.ctx.Styles.PrView.PillStyle.
		BorderForeground(bgColor).
		Background(bgColor).
		Render(m.pr.RenderState())
}

func (m *Model) renderLabels() string {
	width := m.getIndentedContentWidth()
	labels := m.pr.Data.Primary.Labels.Nodes
	style := m.ctx.Styles.PrView.PillStyle
	if len(labels) == 0 {
		return ""
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.ctx.Styles.Common.MainTextStyle.Underline(true).Bold(true).Render(
			fmt.Sprintf("%s Labels", constants.LabelsIcon)),
		"",
		common.RenderLabels(labels, common.LabelOpts{
			Width:     width,
			PillStyle: style,
		}),
	)
}

type reviewerItem struct {
	text string
}

func (m *Model) renderRequestedReviewers() string {
	if !m.pr.Data.IsEnriched {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			m.ctx.Styles.Common.MainTextStyle.Underline(true).Bold(true).Render(
				fmt.Sprintf("%s Reviewers", constants.CodeReviewIcon)),
			"",
			lipgloss.JoinHorizontal(
				lipgloss.Top,
				m.ctx.Styles.Common.WaitingGlyph,
				" ",
				m.ctx.Styles.Common.FaintTextStyle.Render("Loading..."),
			),
		)
	}

	reviewRequests := m.pr.Data.Enriched.ReviewRequests.Nodes
	reviews := m.pr.Data.Enriched.Reviews.Nodes
	suggestedReviewers := m.pr.Data.Enriched.SuggestedReviewers

	if len(reviewRequests) == 0 && len(reviews) == 0 && len(suggestedReviewers) == 0 {
		return ""
	}

	reviewStates := make(map[string]string)
	for _, review := range reviews {
		login := review.Author.Login
		existingState := reviewStates[login]
		// Don't override APPROVED or CHANGES_REQUESTED with COMMENTED
		if review.State == "COMMENTED" &&
			(existingState == "APPROVED" || existingState == "CHANGES_REQUESTED") {
			continue
		}
		reviewStates[login] = review.State
	}

	reviewerItems := make([]reviewerItem, 0)
	faintStyle := m.ctx.Styles.Common.FaintTextStyle
	reviewerStyle := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)
	successStyle := lipgloss.NewStyle().Foreground(m.ctx.Theme.SuccessText)
	errorStyle := lipgloss.NewStyle().Foreground(m.ctx.Theme.ErrorText)

	shownReviewers := make(map[string]bool)

	for _, req := range reviewRequests {
		displayName := req.GetReviewerDisplayName()
		if displayName == "" {
			continue
		}
		shownReviewers[displayName] = true

		var reviewerStr string
		stateIcon := ""
		if state, hasReview := reviewStates[displayName]; hasReview && state == "COMMENTED" {
			stateIcon = m.ctx.Styles.Common.CommentGlyph
		} else {
			stateIcon = m.ctx.Styles.Common.WaitingDotGlyph
		}

		if req.IsTeam() {
			reviewerStr += reviewerStyle.Render(displayName)
		} else {
			reviewerStr += reviewerStyle.Render("@" + displayName)
		}

		if req.AsCodeOwner {
			reviewerStr = lipgloss.JoinHorizontal(lipgloss.Top,
				faintStyle.Render(constants.OwnerIcon), " ", reviewerStr)
		}
		reviewerStr = lipgloss.JoinHorizontal(lipgloss.Top, stateIcon, " ", reviewerStr)

		reviewerItems = append(reviewerItems, reviewerItem{text: reviewerStr})
	}

	for login, state := range reviewStates {
		if shownReviewers[login] {
			continue
		}
		if state != "APPROVED" && state != "CHANGES_REQUESTED" && state != "COMMENTED" {
			continue
		}
		shownReviewers[login] = true

		var stateIcon string
		switch state {
		case "APPROVED":
			stateIcon = successStyle.Render(constants.ApprovedIcon)
		case "CHANGES_REQUESTED":
			stateIcon = errorStyle.Render(constants.ChangesRequestedIcon)
		case "COMMENTED":
			stateIcon = m.ctx.Styles.Common.CommentGlyph
		}
		reviewerStr := stateIcon + " " + reviewerStyle.Render("@"+login)

		reviewerItems = append(reviewerItems, reviewerItem{text: reviewerStr})
	}

	// Show suggested reviewers (= code owners) who haven't been requested or reviewed yet
	for _, suggested := range suggestedReviewers {
		login := suggested.Reviewer.Login
		if shownReviewers[login] {
			continue
		}
		if suggested.IsAuthor {
			continue
		}
		shownReviewers[login] = true

		reviewerStr := lipgloss.JoinHorizontal(lipgloss.Top,
			faintStyle.Render(constants.OwnerIcon), " ",
			faintStyle.Render("@"+login),
		)

		reviewerItems = append(reviewerItems, reviewerItem{text: reviewerStr})
	}

	if len(reviewerItems) == 0 {
		return ""
	}

	width := m.getIndentedContentWidth()
	var rows []string
	var currentRow strings.Builder
	currentRowWidth := 0

	for i, item := range reviewerItems {
		itemWidth := lipgloss.Width(item.text)
		separator := ", "
		separatorWidth := lipgloss.Width(separator)

		// Check if adding this item would exceed the width
		needsSeparator := i < len(reviewerItems)-1
		totalItemWidth := itemWidth
		if needsSeparator {
			totalItemWidth += separatorWidth
		}

		if currentRowWidth > 0 && currentRowWidth+totalItemWidth > width {
			// Start a new row
			rows = append(rows, currentRow.String())
			currentRow.Reset()
			currentRowWidth = 0
		}

		currentRow.WriteString(item.text)
		currentRowWidth += itemWidth

		if needsSeparator {
			currentRow.WriteString(separator)
			currentRowWidth += separatorWidth
		}
	}

	// Add the last row
	if currentRow.Len() > 0 {
		rows = append(rows, currentRow.String())
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.ctx.Styles.Common.MainTextStyle.Underline(true).Bold(true).Render(
			fmt.Sprintf("%s Reviewers", constants.CodeReviewIcon)),
		"",
		strings.Join(rows, "\n"),
	)
}

func (m *Model) renderAuthor() string {
	authorAssociation := m.pr.Data.Primary.AuthorAssociation
	if authorAssociation == "" {
		authorAssociation = "unknown role"
	}
	time := lipgloss.NewStyle().Render(utils.TimeElapsed(m.pr.Data.Primary.CreatedAt))
	return lipgloss.JoinHorizontal(lipgloss.Top,
		" by ",
		lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText).Render(
			lipgloss.NewStyle().Bold(true).Render("@"+m.pr.Data.Primary.Author.Login)),
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			lipgloss.JoinHorizontal(lipgloss.Top, " ⋅ ", time, " ago", " ⋅ ")),
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			lipgloss.JoinHorizontal(lipgloss.Top, data.GetAuthorRoleIcon(m.pr.Data.Primary.AuthorAssociation,
				m.ctx.Theme),
				" ", lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(strings.ToLower(authorAssociation))),
		),
	)
}

func (m *Model) renderSummary() string {
	width := m.getIndentedContentWidth()
	// Strip HTML comments from body and cleanup body.
	body := htmlCommentRegex.ReplaceAllString(m.pr.Data.Enriched.Body, "")
	body = lineCleanupRegex.ReplaceAllString(body, "")

	desc := m.ctx.Styles.Common.MainTextStyle.Bold(true).Underline(true).Render(" Summary")
	title := lipgloss.JoinVertical(
		lipgloss.Left,
		desc,
		"",
	)
	sbody := lipgloss.NewStyle().Width(m.getIndentedContentWidth())
	body = strings.TrimSpace(body)
	if body == "" {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			sbody.Italic(true).Foreground(m.ctx.Theme.FaintText).Render("No description provided."),
		)
	}

	markdownRenderer := markdown.GetMarkdownRenderer(width, m.ctx)
	rendered, err := markdownRenderer.Render(body)
	if err != nil {
		return ""
	}

	bodyHeight := lipgloss.Height(rendered)
	if !m.summaryViewMore && bodyHeight > foldBodyHeight {
		rendered = lipgloss.NewStyle().MaxHeight(foldBodyHeight).Render(rendered)
		rendered = lipgloss.JoinVertical(lipgloss.Left,
			rendered,
			"",
			lipgloss.PlaceHorizontal(m.getIndentedContentWidth(), lipgloss.Center,
				lipgloss.JoinHorizontal(
					lipgloss.Top,
					lipgloss.NewStyle().Bold(true).Italic(true).Render("Press "),
					lipgloss.NewStyle().
						Background(m.ctx.Theme.SelectedBackground).
						Foreground(m.ctx.Theme.PrimaryText).
						Render("e"),
					lipgloss.NewStyle().Bold(true).Italic(true).Render(" to read more..."),
				),
			),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left, title,
		lipgloss.NewStyle().
			Width(width).
			MaxWidth(width).
			Align(lipgloss.Left).
			Render(rendered),
	)
}

func (m *Model) SetSectionId(id int) {
	m.sectionId = id
}

func (m *Model) SetRow(d *prrow.Data) {
	if m.pr == nil || d == nil || m.pr.Data.Primary.Url != d.Primary.Url {
		// What's expanded and narrowed to belongs to the previous PR
		m.expandedCommit = ""
		m.expandedCheck = ""
		m.commitFiles = nil
		m.syncTabs()
	}
	if d == nil {
		m.pr = nil
	} else {
		m.pr = &prrow.PullRequest{Ctx: m.ctx, Data: d}
	}
}

type EnrichedPrMsg struct {
	Id   int
	Type string
	Data data.EnrichedPullRequestData
	Err  error
}

func (m *Model) EnrichCurrRow() tea.Cmd {
	if m == nil || m.pr == nil || m.pr.Data.IsEnriched {
		return nil
	}
	url := m.pr.Data.Primary.Url
	return func() tea.Msg {
		d, err := data.FetchPullRequest(url)
		return EnrichedPrMsg{
			Id:   m.sectionId,
			Type: prssection.SectionType,
			Data: d,
			Err:  err,
		}
	}
}

func (m *Model) SetWidth(width int) {
	m.width = width
	m.tabsHint = m.renderTabsHint()
	// Only show the hint when it fits alongside all the tabs
	if lipgloss.Width(m.tabsHint)+m.carousel.ItemsWidth() > width {
		m.tabsHint = ""
	}
	m.carousel.SetWidth(width - lipgloss.Width(m.tabsHint))
	m.editor.SetWidth(
		m.getIndentedContentWidth() - m.ctx.Styles.Sidebar.InputBox.GetHorizontalFrameSize(),
	)
}

func (m *Model) IsTextInputBoxFocused() bool {
	return m.editor.Active() && !m.editor.Detached()
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = ctx
	m.editor.UpdateProgramContext(ctx)
	m.carousel.SetStyles(
		carousel.Styles{
			Item:     lipgloss.NewStyle().Padding(0, 1).Foreground(m.ctx.Theme.FaintText),
			Selected: lipgloss.NewStyle().Padding(0, 1).Bold(true),
		},
	)

	// TODO: move this to the NewModel func
	// currently it's not possible since the styles aren't yet instantiated when NewModel is called
	m.editor.SetSelectStyles(ctx.Styles.Select)
}

func (m *Model) GetIsCommenting() bool {
	return m.editor.Mode() == cmpcontroller.ModeComment
}

func (m *Model) SetIsCommenting(isCommenting bool) tea.Cmd {
	if !isCommenting {
		if m.pr != nil && m.editor.Mode() == cmpcontroller.ModeComment {
			m.editor.Exit()
		}
		return nil
	}
	return m.StartComment("", false)
}

// StartComment opens the comment editor with text already in it, e.g. a
// quoted comment to reply to. When detachable, esc leaves the editor keeping
// the draft, rather than cancelling.
func (m *Model) StartComment(text string, detachable bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	m.editor.SetAutocompleteSource(&fuzzyselect.UserMentionSource{WithAtSymbol: true})
	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		InitialValue:                     text,
		Mode:                             cmpcontroller.ModeComment,
		Prompt:                           constants.CommentPrompt,
		Repo:                             m.repoRef(),
		EnterFetch:                       cmpcontroller.FetchSilent,
		ConfirmDiscardOnCancel:           true,
		HideAutocompleteWhenContextEmpty: true,
		Detachable:                       detachable,
	})
	return cmd
}

func (m *Model) getIndentedContentWidth() int {
	return m.width - 2*m.ctx.Styles.Sidebar.ContentPadding
}

func (m *Model) GetIsApproving() bool {
	return m.editor.Mode() == cmpcontroller.ModeApprove
}

func (m *Model) SetIsApproving(isApproving bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isApproving {
		if m.editor.Mode() == cmpcontroller.ModeApprove {
			m.editor.Exit()
		}
		return nil
	}

	m.editor.SetAutocompleteSource(&fuzzyselect.UserMentionSource{WithAtSymbol: true})
	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeApprove,
		Prompt:                           constants.ApprovalPrompt,
		InitialValue:                     m.ctx.Config.Defaults.PrApproveComment,
		Repo:                             m.repoRef(),
		EnterFetch:                       cmpcontroller.FetchSilent,
		ConfirmDiscardOnCancel:           true,
		HideAutocompleteWhenContextEmpty: true,
	})
	return cmd
}

func (m *Model) GetIsAssigning() bool {
	return m.editor.Mode() == cmpcontroller.ModeAssign
}

func (m *Model) SetIsAssigning(isAssigning bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isAssigning {
		if m.editor.Mode() == cmpcontroller.ModeAssign {
			m.editor.Exit()
		}
		return nil
	}

	initialValue := ""
	if !m.userAssignedToPr(m.ctx.User) {
		initialValue = m.ctx.User
	}

	m.editor.SetAutocompleteSource(&fuzzyselect.UserMentionSource{WithAtSymbol: false})
	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeAssign,
		Prompt:                           constants.AssignPrompt,
		InitialValue:                     initialValue,
		Repo:                             m.repoRef(),
		EnterFetch:                       cmpcontroller.FetchSilent,
		HideAutocompleteWhenContextEmpty: false,
	})
	m.editor.ShowCompletions()
	return cmd
}

func (m *Model) userAssignedToPr(login string) bool {
	for _, a := range m.pr.Data.Primary.Assignees.Nodes {
		if login == a.Login {
			return true
		}
	}
	return false
}

func (m *Model) GetIsUnassigning() bool {
	return m.editor.Mode() == cmpcontroller.ModeUnassign
}

func (m *Model) SetIsUnassigning(isUnassigning bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isUnassigning {
		if m.editor.Mode() == cmpcontroller.ModeUnassign {
			m.editor.Exit()
		}
		return nil
	}

	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:         cmpcontroller.ModeUnassign,
		Prompt:       constants.UnassignPrompt,
		InitialValue: strings.Join(m.prAssignees(), "\n"),
		Repo:         m.repoRef(),
	})
	return cmd
}

func (m *Model) prAssignees() []string {
	var assignees []string
	for _, n := range m.pr.Data.Primary.Assignees.Nodes {
		assignees = append(assignees, n.Login)
	}
	return assignees
}

func (m *Model) GoToFirstTab() {
	m.setTab(overviewTab)
}

func (m *Model) GoToActivityTab() {
	m.setTab(activityTab)
}

func (m *Model) PrevTab() {
	m.setTab(max(0, m.carousel.Cursor()-1))
}

func (m *Model) NextTab() {
	m.setTab(min(len(tabs)-1, m.carousel.Cursor()+1))
}

// setTab selects a tab. Leaving the files tab shows all of the PR's files
// again. The expanded commit is kept, to focus it again on coming back.
func (m *Model) setTab(tab int) {
	m.carousel.SetCursor(tab)
	if tab != filesTab {
		m.commitFiles = nil
	}
	m.syncTabs()
}

// syncTabs names the tabs, with the files tab naming the commit it's narrowed
// to, e.g. "Files Changed (abc1234)".
func (m *Model) syncTabs() {
	items := slices.Clone(tabs)
	if m.commitFiles != nil {
		items[filesTab] += " (" + m.commitFiles.abbreviatedOid + ")"
	}
	if slices.Equal(items, m.carousel.Items()) {
		return
	}
	m.carousel.SetItems(items)
	// The tabs' width changed, which decides whether the hint fits
	m.SetWidth(m.width)
}

// IsCommitsTab reports whether the commits tab is selected.
func (m Model) IsCommitsTab() bool {
	return m.carousel.Cursor() == commitsTab
}

// SetFocusedCommit expands the commit at the given index to show its full
// message, collapsing any other. It reports whether that changed anything.
func (m *Model) SetFocusedCommit(i int) bool {
	oid := ""
	if m.hasData() {
		if commits := m.pr.Data.Enriched.AllCommits.Nodes; i >= 0 && i < len(commits) {
			oid = commits[i].Commit.Oid
		}
	}
	if oid == m.expandedCommit {
		return false
	}
	m.expandedCommit = oid
	return true
}

// ExpandedCommitIndex returns the index of the commit shown with its full
// message, or -1 when there's none.
func (m Model) ExpandedCommitIndex() int {
	if !m.hasData() || m.expandedCommit == "" {
		return -1
	}
	for i, c := range m.pr.Data.Enriched.AllCommits.Nodes {
		if c.Commit.Oid == m.expandedCommit {
			return i
		}
	}
	return -1
}

// ViewCommitFiles narrows the files tab to the files changed by the commit at
// the given index and switches to it, returning the command fetching them.
// With only one commit, it just switches to the files tab.
func (m *Model) ViewCommitFiles(i int) tea.Cmd {
	if !m.hasData() {
		return nil
	}
	commits := m.pr.Data.Enriched.AllCommits.Nodes
	if i < 0 || i >= len(commits) {
		return nil
	}
	m.setTab(filesTab)
	if len(commits) == 1 {
		// The commit's files are the PR's files
		return nil
	}
	commit := commits[i].Commit
	m.commitFiles = &commitFiles{
		oid:            commit.Oid,
		abbreviatedOid: commit.AbbreviatedOid,
		loading:        true,
	}
	m.syncTabs()

	repo := m.pr.Data.Primary.GetRepoNameWithOwner()
	oid := commit.Oid
	return func() tea.Msg {
		files, err := data.FetchCommitFiles(repo, oid)
		return CommitFilesMsg{Oid: oid, Files: files, Err: err}
	}
}

// SetCommitFiles shows the fetched files of the commit the files tab is
// narrowed to. Files of a commit that's no longer shown are dropped.
func (m *Model) SetCommitFiles(msg CommitFilesMsg) {
	if m.commitFiles == nil || m.commitFiles.oid != msg.Oid {
		return
	}
	m.commitFiles.loading = false
	m.commitFiles.files = msg.Files
	m.commitFiles.err = msg.Err
}

// IsFirstTab reports whether the first tab, the overview, is selected.
func (m Model) IsFirstTab() bool {
	return m.carousel.Cursor() == 0
}

func (m Model) SelectedTab() string {
	return m.carousel.SelectedItem()
}

func (m *Model) SetSummaryViewMore() {
	m.summaryViewMore = true
}

func (m *Model) SetSummaryViewLess() {
	m.summaryViewMore = false
}

func (m *Model) SetEnrichedPR(data data.EnrichedPullRequestData) {
	if m.pr.Data.Primary.Url == data.Url {
		m.pr.Data.Enriched = data
		m.pr.Data.IsEnriched = true
	}
}

func (m *Model) GetIsLabeling() bool {
	return m.editor.Mode() == cmpcontroller.ModeLabel
}

// SetIsLabeling enters or exits labeling mode
func (m *Model) SetIsLabeling(isLabeling bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isLabeling {
		if m.editor.Mode() == cmpcontroller.ModeLabel {
			m.editor.Exit()
		}
		return nil
	}

	labels := make([]string, 0, len(m.pr.Data.Primary.Labels.Nodes)+1)
	for _, label := range m.pr.Data.Primary.Labels.Nodes {
		labels = append(labels, label.Name)
	}
	labels = append(labels, "")

	m.editor.SetAutocompleteSource(&fuzzyselect.LabelSource{})
	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeLabel,
		Prompt:                           constants.LabelPrompt,
		InitialValue:                     strings.Join(labels, ", "),
		Repo:                             m.repoRef(),
		EnterFetch:                       cmpcontroller.FetchSilent,
		HideAutocompleteWhenContextEmpty: false,
		ConfirmDiscardOnCancel:           false,
	})
	m.editor.ShowCompletions()
	return cmd
}

func (m *Model) repoRef() cmpcontroller.RepoRef {
	owner, repo := m.pr.Data.Primary.GetRepoNameAndOwner()
	return cmpcontroller.RepoRef{
		NameWithOwner: m.pr.Data.Primary.GetRepoNameWithOwner(),
		Owner:         owner,
		Name:          repo,
	}
}

func (m *Model) hasData() bool {
	return m.pr != nil && m.pr.Data != nil
}
