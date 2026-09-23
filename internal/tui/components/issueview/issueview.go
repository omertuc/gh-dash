package issueview

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/cmpcontroller"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/fuzzyselect"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/inputbox"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuerow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuessection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

var (
	htmlCommentRegex = regexp.MustCompile("(?U)<!--(.|[[:space:]])*-->")
)

type Model struct {
	ctx       *context.ProgramContext
	issue     *issuerow.Issue
	sectionId int
	width     int
	editor    cmpcontroller.Controller
}

func NewModel(ctx *context.ProgramContext) Model {
	ta := inputbox.DefaultTextArea(ctx)
	cmp := cmpcontroller.New(ctx, inputbox.ModelOpts{TextArea: &ta})

	return Model{
		ctx:    ctx,
		issue:  nil,
		editor: cmp,
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd, *IssueAction) {
	cmd, handled := m.editor.Update(msg)

	if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "ctrl+d" {
		value := m.editor.Value()
		mode := m.editor.Mode()
		m.editor.Exit()
		if m.issue == nil {
			return m, nil, nil
		}

		sid := tasks.SectionIdentifier{Id: m.sectionId, Type: issuessection.SectionType}

		switch mode {
		case cmpcontroller.ModeComment:
			if len(strings.TrimSpace(value)) != 0 {
				return m, tasks.CommentOnIssue(m.ctx, sid, m.issue.Data, value), nil
			}
			return m, nil, nil

		case cmpcontroller.ModeAssign:
			usernames := fuzzyselect.AllWords(value)
			if len(usernames) > 0 {
				return m, tasks.AssignIssue(m.ctx, sid, m.issue.Data, usernames), nil
			}
			return m, nil, nil

		case cmpcontroller.ModeUnassign:
			usernames := fuzzyselect.AllWords(value)
			if len(usernames) > 0 {
				return m, tasks.UnassignIssue(m.ctx, sid, m.issue.Data, usernames), nil
			}
			return m, nil, nil

		case cmpcontroller.ModeLabel:
			labels := fuzzyselect.CurrentLabels(value)
			if len(labels) > 0 || len(m.issue.Data.Labels.Nodes) > 0 {
				return m, tasks.LabelIssue(
					m.ctx,
					sid,
					m.issue.Data,
					labels,
					m.issue.Data.Labels.Nodes,
				), nil
			}
			return m, nil, nil
		}
	}
	if handled {
		return m, cmd, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, keys.IssueKeys.Label):
			return m, nil, &IssueAction{Type: IssueActionLabel}
		case key.Matches(keyMsg, keys.IssueKeys.Assign):
			return m, nil, &IssueAction{Type: IssueActionAssign}
		case key.Matches(keyMsg, keys.IssueKeys.Unassign):
			return m, nil, &IssueAction{Type: IssueActionUnassign}
		case key.Matches(keyMsg, keys.IssueKeys.Comment):
			return m, nil, &IssueAction{Type: IssueActionComment}
		case key.Matches(keyMsg, keys.IssueKeys.Checkout):
			return m, nil, &IssueAction{Type: IssueActionCheckout}
		case key.Matches(keyMsg, keys.IssueKeys.Close):
			return m, nil, &IssueAction{Type: IssueActionClose}
		case key.Matches(keyMsg, keys.IssueKeys.Reopen):
			return m, nil, &IssueAction{Type: IssueActionReopen}
		}
	}

	return m, cmd, nil
}

func (m Model) View() string {
	return m.ViewHeader() + "\n" + m.ViewBody()
}

// ViewHeader renders the top of the preview: the issue's name, title, status
// and author.
func (m Model) ViewHeader() string {
	s := strings.Builder{}

	s.WriteString(m.renderFullNameAndNumber())
	s.WriteString("\n")

	s.WriteString(m.renderTitle())
	s.WriteString("\n\n")
	s.WriteString(m.renderStatusPill())
	s.WriteString("\n\n")
	s.WriteString(m.renderAuthor())

	// End with a blank line to separate the header from the body
	return m.contentStyle().Render(s.String()) + "\n"
}

// ViewBody renders the rest of the preview below the header: labels, the
// issue's description and its comments.
func (m Model) ViewBody() string {
	body, _ := m.ViewBodyWithAnchors()
	return body
}

// ViewBodyWithAnchors renders the body along with where each comment starts.
func (m Model) ViewBodyWithAnchors() (string, []common.CommentAnchor) {
	s := strings.Builder{}

	labels := m.renderLabels()
	if labels != "" {
		s.WriteString(labels)
		s.WriteString("\n\n")
	}

	s.WriteString(m.renderBody())
	s.WriteString("\n\n")
	activityStart := strings.Count(s.String(), "\n")
	activity, anchors := m.renderActivityWithAnchors()
	s.WriteString(activity)
	for i := range anchors {
		anchors[i].Line += activityStart
	}

	return m.contentStyle().Render(s.String()), anchors
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

func (m Model) contentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding)
}

func (m *Model) ViewCompletions() string {
	if !m.hasData() {
		return ""
	}

	return m.editor.ViewCompletions()
}

func (m *Model) InputBoxLineFromButton() int {
	return m.editor.LineFromBottom()
}

func (m *Model) renderFullNameAndNumber() string {
	return common.RenderPreviewHeader(m.ctx.Theme, m.width,
		fmt.Sprintf("#%d · %s", m.issue.Data.GetNumber(), m.issue.Data.GetRepoNameWithOwner()))
}

func (m *Model) renderTitle() string {
	return common.RenderPreviewTitle(m.ctx.Theme, m.ctx.Styles.Common, m.width, m.issue.Data.Title)
}

func (m *Model) renderStatusPill() string {
	var bgColor color.Color
	content := ""
	switch m.issue.Data.State {
	case "OPEN":
		bgColor = m.ctx.Styles.Colors.OpenIssue.Dark
		content = " Open"
	case "CLOSED":
		bgColor = m.ctx.Styles.Colors.ClosedIssue.Dark
		content = " Closed"
	}

	return m.ctx.Styles.PrView.PillStyle.
		BorderForeground(bgColor).
		Background(bgColor).
		Render(content)
}

func (m *Model) renderAuthor() string {
	authorAssociation := m.issue.Data.AuthorAssociation
	if authorAssociation == "" {
		authorAssociation = "unknown role"
	}
	time := lipgloss.NewStyle().Render(utils.TimeElapsed(m.issue.Data.CreatedAt))
	return lipgloss.JoinHorizontal(lipgloss.Top,
		" by ",
		lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText).Render(
			lipgloss.NewStyle().Bold(true).Render("@"+m.issue.Data.Author.Login)),
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			lipgloss.JoinHorizontal(lipgloss.Top, " ⋅ ", time, " ago", " ⋅ ")),
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			lipgloss.JoinHorizontal(lipgloss.Top, data.GetAuthorRoleIcon(m.issue.Data.AuthorAssociation,
				m.ctx.Theme),
				" ", lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(strings.ToLower(authorAssociation))),
		),
	)
}

func (m *Model) renderBody() string {
	width := m.getIndentedContentWidth()
	body := bodyMarkdown(m.issue.Data.Body)
	if body == "" {
		return lipgloss.NewStyle().
			Italic(true).
			Foreground(m.ctx.Theme.FaintText).
			Render("No description provided.")
	}

	markdownRenderer := markdown.GetMarkdownRenderer(width, m.ctx)
	rendered, err := markdownRenderer.Render(body)
	if err != nil {
		return ""
	}

	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Align(lipgloss.Left).
		Render(rendered)
}

func (m *Model) renderLabels() string {
	width := m.getIndentedContentWidth()
	labels := m.issue.Data.Labels.Nodes
	style := m.ctx.Styles.PrView.PillStyle

	return common.RenderLabels(labels, common.LabelOpts{
		Width:     width,
		PillStyle: style,
	})
}

func (m *Model) getIndentedContentWidth() int {
	return indentedContentWidth(m.width)
}

func indentedContentWidth(width int) int {
	return width - 6
}

// commentsContentWidth is the width comments are rendered at, given the
// preview's width.
func commentsContentWidth(width int) int {
	return indentedContentWidth(width) - 2
}

// bodyMarkdown is the markdown shown for an issue's description, without
// HTML comments.
func bodyMarkdown(body string) string {
	return strings.TrimSpace(htmlCommentRegex.ReplaceAllString(body, ""))
}

// MarkdownPrerenderer returns a function that renders the markdown the
// preview shows for issue, at the given preview width, into the markdown
// cache, so showing it later is instant. It reads ctx, so it must be created
// on the UI goroutine, while the function it returns is meant to run in the
// background.
func MarkdownPrerenderer(ctx *context.ProgramContext, issue *data.IssueData, width int) func() {
	bodyRenderer := markdown.GetMarkdownRenderer(indentedContentWidth(width), ctx)
	commentsRenderer := markdown.GetMarkdownRenderer(commentsContentWidth(width), ctx)
	body := bodyMarkdown(issue.Body)
	comments := make([]string, 0, len(issue.Comments.Nodes))
	for _, c := range issue.Comments.Nodes {
		comments = append(comments, c.Body)
	}
	return func() {
		if body != "" {
			bodyRenderer.Prerender(body)
		}
		commentsRenderer.Prerender(comments...)
	}
}

func (m *Model) SetWidth(width int) {
	m.width = width
	m.editor.SetWidth(
		m.getIndentedContentWidth() - m.ctx.Styles.Sidebar.InputBox.GetHorizontalFrameSize(),
	)
}

func (m *Model) SetSectionId(id int) {
	m.sectionId = id
}

func (m *Model) SetRow(data *data.IssueData) {
	if data == nil {
		m.issue = nil
	} else {
		m.issue = &issuerow.Issue{Ctx: m.ctx, Data: *data}
	}
}

func (m *Model) IsTextInputBoxFocused() bool {
	return m.editor.Active() && !m.editor.Detached()
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = ctx
	m.editor.UpdateProgramContext(ctx)

	// TODO: move this to the NewModel func
	// currently it's not possible since the styles aren't yet instantiated when NewModel is called
	m.editor.SetSelectStyles(ctx.Styles.Select)
}

func (m *Model) GetIsCommenting() bool {
	return m.editor.Mode() == cmpcontroller.ModeComment
}

func (m *Model) SetIsCommenting(isCommenting bool) tea.Cmd {
	if !isCommenting {
		if m.issue != nil && m.editor.Mode() == cmpcontroller.ModeComment {
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
	if m.issue == nil {
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

func (m *Model) GetIsAssigning() bool {
	return m.editor.Mode() == cmpcontroller.ModeAssign
}

func (m *Model) SetIsAssigning(isAssigning bool) tea.Cmd {
	if m.issue == nil {
		return nil
	}

	if !isAssigning {
		if m.editor.Mode() == cmpcontroller.ModeAssign {
			m.editor.Exit()
		}
		return nil
	}

	initialValue := ""
	if !m.userAssignedToIssue(m.ctx.User) {
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
	return cmd
}

func (m *Model) SetIsLabeling(isLabeling bool) tea.Cmd {
	if m.issue == nil {
		return nil
	}

	if !isLabeling {
		if m.editor.Mode() == cmpcontroller.ModeLabel {
			m.editor.Exit()
		}
		return nil
	}

	labels := make([]string, 0, len(m.issue.Data.Labels.Nodes)+1)
	for _, label := range m.issue.Data.Labels.Nodes {
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
	})
	m.editor.ShowCompletions()
	return cmd
}

func (m *Model) userAssignedToIssue(login string) bool {
	for _, a := range m.issue.Data.Assignees.Nodes {
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
	if m.issue == nil {
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
		InitialValue: strings.Join(m.issueAssignees(), "\n"),
		Repo:         m.repoRef(),
	})
	return cmd
}

func (m *Model) issueAssignees() []string {
	var assignees []string
	for _, n := range m.issue.Data.Assignees.Nodes {
		assignees = append(assignees, n.Login)
	}
	return assignees
}

func (m *Model) repoRef() cmpcontroller.RepoRef {
	owner, repo := m.issue.Data.GetRepoNameAndOwner()
	return cmpcontroller.RepoRef{
		NameWithOwner: m.issue.Data.GetRepoNameWithOwner(),
		Owner:         owner,
		Name:          repo,
	}
}

func (m *Model) hasData() bool {
	return m.issue != nil
}
