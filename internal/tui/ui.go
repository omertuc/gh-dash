package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	log "charm.land/log/v2"
	"github.com/atotto/clipboard"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/cli/go-gh/v2/pkg/repository"
	zone "github.com/lrstanley/bubblezone/v2"

	gitm "github.com/aymanbagabas/git-module"
	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/git"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branch"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branchsidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuessection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/reposection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tabs"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

type Model struct {
	keys             *keys.KeyMap
	sidebar          sidebar.Model
	prView           prview.Model
	issueSidebar     issueview.Model
	branchSidebar    branchsidebar.Model
	notificationView notificationview.Model
	currSectionId    int
	footer           footer.Model
	repo             section.Section
	prs              []section.Section
	issues           []section.Section
	notifications    []section.Section
	tabs             tabs.Model
	ctx              *context.ProgramContext
	taskSpinner      spinner.Model
	tasks            map[string]context.Task
	positionOverride string // "" means no override, "right" or "bottom"
	mode             Mode

	// sidebarComments are the comments shown in the sidebar, in the order
	// the sidebar's anchors are in
	sidebarComments []common.CommentAnchor

	// drafts are unsent comments kept for notifications that were left
	// while writing them, by notification id
	drafts map[string]string
	// confirmingDraftDiscard is whether discarding a draft awaits a y/n
	confirmingDraftDiscard bool
}

type Mode int

const (
	ModeNone Mode = iota
	ModeSection
)

type Repositories struct {
	GHRepo  *repository.Repository
	GitRepo *gitm.Repository
}

func NewModel(location config.Location, repos Repositories) Model {
	taskSpinner := spinner.Model{Spinner: spinner.Dot}
	m := Model{
		keys:        keys.Keys,
		sidebar:     sidebar.NewModel(),
		taskSpinner: taskSpinner,
		tasks:       map[string]context.Task{},
	}

	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Sum != "" {
		version = info.Main.Version
	}

	m.ctx = &context.ProgramContext{
		GHRepo:     repos.GHRepo,
		GitRepo:    repos.GitRepo,
		ConfigFlag: location.ConfigFlag,
		RepoPath:   location.RepoPath,
		Version:    version,
		StartTask: func(task context.Task) tea.Cmd {
			log.Info("Starting task", "id", task.Id)
			task.StartTime = time.Now()
			m.tasks[task.Id] = task
			return m.taskSpinner.Tick
		},
		HasDarkBackground: true,
		BackgroundSource:  "default",
		Theme:             *theme.DefaultTheme,
		Styles:            context.DefaultStyles,
	}

	m.footer = footer.NewModel(m.ctx)
	m.prView = prview.NewModel(m.ctx)
	m.issueSidebar = issueview.NewModel(m.ctx)
	m.branchSidebar = branchsidebar.NewModel(m.ctx)
	m.notificationView = notificationview.NewModel(m.ctx)
	m.tabs = tabs.NewModel(m.ctx)

	return m
}

func (m *Model) initScreen() tea.Msg {
	showError := func(err error) {
		styles := log.DefaultStyles()
		styles.Key = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true)
		styles.Separator = lipgloss.NewStyle()

		logger := log.New(os.Stderr)
		logger.SetStyles(styles)
		logger.SetTimeFormat(time.RFC3339)
		logger.SetReportTimestamp(true)
		logger.SetPrefix("Reading config file")
		logger.SetReportCaller(true)

		logger.
			Fatal(
				"failed parsing config file",
				"location",
				m.ctx.ConfigFlag,
				"err",
				err,
			)
	}

	cfg, err := config.ParseConfig(
		config.Location{RepoPath: m.ctx.RepoPath, ConfigFlag: m.ctx.ConfigFlag},
	)
	if err != nil {
		showError(err)
		return initMsg{Config: cfg}
	}

	var url string
	if config.IsFeatureEnabled(config.FF_REPO_VIEW) && m.ctx.RepoPath != "" {
		res, err := git.GetOriginUrl(m.ctx.RepoPath)
		if err != nil {
			showError(err)
			return initMsg{Config: cfg}
		}
		url = res
	}

	err = keys.Rebind(
		cfg.Keybindings.Universal,
		cfg.Keybindings.Issues,
		cfg.Keybindings.Prs,
		cfg.Keybindings.Branches,
		cfg.Keybindings.Notifications,
		cfg.Keybindings.Cmp,
	)
	if err != nil {
		showError(err)
	}

	return initMsg{Config: cfg, RepoUrl: url}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tea.RequestBackgroundColor,
		// Ask the terminal to notify us when its color scheme changes, so
		// we can switch between light and dark styles live.
		tea.Raw(ansi.SetModeLightDark),
		// Check whether the terminal supports that; if not, we poll instead.
		tea.Raw(ansi.RequestModeLightDark),
		m.initScreen,
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd             tea.Cmd
		tabsCmd         tea.Cmd
		sidebarCmd      tea.Cmd
		prViewCmd       tea.Cmd
		issueSidebarCmd tea.Cmd
		footerCmd       tea.Cmd
		cmds            []tea.Cmd
		currSection     = m.getCurrSection()
		currRowData     = m.getCurrRowData()
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		log.Info("Key pressed", "key", msg.String())
		m.ctx.Error = nil

		if currSection != nil && (currSection.IsSearchFocused() ||
			currSection.IsPromptConfirmationFocused()) {
			cmd = m.updateSection(currSection.GetId(), currSection.GetType(), msg)
			return m, cmd
		}

		if m.prView.IsTextInputBoxFocused() {
			m.prView, cmd = m.prView.Update(msg)
			m.syncSidebar()
			return m, cmd
		}

		if m.issueSidebar.IsTextInputBoxFocused() {
			m.issueSidebar, cmd, _ = m.issueSidebar.Update(msg)
			m.syncSidebar()
			return m, cmd
		}

		if m.sidebar.IsSearching() {
			cmd = m.sidebar.UpdateSearch(msg)
			m.syncFocusedCommit(false)
			return m, cmd
		}

		if m.footer.ShowConfirmQuit && (msg.String() == "y" || msg.String() == "enter") {
			return m, tea.Quit
		} else if m.footer.ShowConfirmQuit {
			m.footer.SetShowConfirmQuit(false)
			return m, nil
		}

		// Handle notification PR/Issue action confirmation
		if m.notificationView.HasPendingAction() {
			var action string
			m.notificationView, action = m.notificationView.Update(msg)
			m.footer.SetLeftSection("")
			if action != "" {
				return m, m.executeNotificationAction(action)
			}
			return m, nil
		}

		// Confirm discarding a detached comment draft
		if m.confirmingDraftDiscard {
			m.confirmingDraftDiscard = false
			if msg.String() == "y" || msg.String() == "Y" {
				m.discardDraft()
			} else {
				m.syncSidebar()
			}
			return m, nil
		}

		// While the help is open, q and esc close it instead of quitting or
		// going back. Ctrl+c still quits.
		if m.footer.ShowAll && msg.String() != "ctrl+c" &&
			(key.Matches(msg, m.keys.Quit) || msg.String() == "esc") {
			m.footer.ShowAll = false
			m.syncMainContentDimensions()
			return m, nil
		}

		switch {
		case m.isUserDefinedKeybinding(msg):
			cmd = m.executeKeybinding(msg.String())
			return m, cmd

		case m.mode == ModeSection:
			if key.Matches(msg, m.keys.NewSection) {
				m.addNewSection()
			}

			if key.Matches(msg, m.keys.RemoveSection) {
				m.removeSection()
			}

			// always exit section mode if any key is pressed after
			m.mode = ModeNone

		case key.Matches(msg, m.keys.SectionMode):
			m.mode = ModeSection

		// With a notification's PR/Issue open, search its comments rather
		// than the notifications
		case key.Matches(msg, m.keys.Search) && m.isNotificationSubjectShown():
			return m, m.sidebar.StartSearch()

		case m.isNotificationSubjectShown() && m.sidebar.HasSearch() &&
			(key.Matches(msg, keys.NotificationKeys.NextMatch) ||
				key.Matches(msg, keys.NotificationKeys.PrevMatch)):
			if key.Matches(msg, keys.NotificationKeys.NextMatch) {
				m.sidebar.SearchNext()
			} else {
				m.sidebar.SearchPrev()
			}
			m.syncFocusedCommit(false)
			return m, nil

		// Esc drops the search before going back to the notification
		case m.isNotificationSubjectShown() && m.sidebar.HasSearch() &&
			key.Matches(msg, keys.NotificationKeys.BackToNotification):
			m.sidebar.ClearSearch()
			return m, nil

		// With a notification's PR/Issue open, refresh it rather than the
		// notifications, e.g. to see new comments
		case key.Matches(msg, m.keys.Refresh) && m.isNotificationSubjectShown():
			return m, m.refreshNotificationSubject()

		// With a comment focused in an open notification, reply to it
		case key.Matches(msg, keys.NotificationKeys.ReplyToComment) && m.hasFocusedComment():
			return m, m.replyToFocusedComment()

		// With a commit focused in an open notification's PR, show its files
		case key.Matches(msg, keys.NotificationKeys.ViewCommitFiles) && m.hasFocusedCommit():
			return m, m.viewFocusedCommitFiles()

		// With a check focused in an open notification's PR, open it
		case key.Matches(msg, keys.NotificationKeys.OpenCheck) && m.hasFocusedCheck():
			return m, m.openFocusedCheck()

		case key.Matches(msg, keys.NotificationKeys.ContinueDraft) && m.hasDetachedDraft():
			return m, m.continueDraft()

		case key.Matches(msg, keys.NotificationKeys.DiscardDraft) && m.hasDetachedDraft():
			m.confirmingDraftDiscard = true
			m.syncSidebar()
			return m, nil

		// With a notification's PR open, h/l switch its tabs rather than moving
		// to another section, which would close it
		case m.isNotificationSubjectShown() && m.notificationView.GetSubjectPR() != nil &&
			(key.Matches(msg, m.keys.PrevSection) || key.Matches(msg, m.keys.NextSection)):
			if key.Matches(msg, m.keys.PrevSection) {
				if m.prView.IsFirstTab() {
					// There's nothing further left, so go back to the list
					return m, m.backToNotification()
				}
				m.prView.PrevTab()
			} else {
				m.prView.NextTab()
			}
			m.syncSidebar()

		case key.Matches(msg, m.keys.PrevSection):
			prevSection := m.getSectionAt(m.getPrevSectionId())
			if prevSection != nil {
				m.setCurrSectionId(prevSection.GetId())
				cmd = tea.Batch(m.onViewedRowChanged(), m.resumeLoadingSpinner())
			}

		case key.Matches(msg, m.keys.NextSection):
			nextSectionId := m.getNextSectionId()
			nextSection := m.getSectionAt(nextSectionId)
			if nextSection != nil {
				m.setCurrSectionId(nextSection.GetId())
				cmd = tea.Batch(m.onViewedRowChanged(), m.resumeLoadingSpinner())
			}

		// With a notification's PR/Issue open, navigation keys scroll within it
		// rather than moving to another notification, which would close it.
		// Esc goes back to the notification list.
		case m.isNotificationSubjectShown() && (key.Matches(msg, m.keys.Down) ||
			key.Matches(msg, m.keys.Up) || key.Matches(msg, m.keys.FirstLine) ||
			key.Matches(msg, m.keys.LastLine)):
			// Move between comments where there are any, else scroll by lines
			switch {
			case key.Matches(msg, m.keys.Down):
				if !m.sidebar.FocusNext() {
					m.sidebar.ScrollDown(previewScrollLines)
				}
			case key.Matches(msg, m.keys.Up):
				if !m.sidebar.FocusPrev() {
					m.sidebar.ScrollUp(previewScrollLines)
				}
			case key.Matches(msg, m.keys.FirstLine):
				m.sidebar.ScrollToTop()
				m.sidebar.ResetFocus()
				// So the first commit is focused, rather than the last one
				m.prView.SetFocusedCommit(-1)
				m.prView.SetFocusedCheck(-1)
			case key.Matches(msg, m.keys.LastLine):
				m.sidebar.ScrollToBottom()
				m.sidebar.FocusLast()
			}
			m.syncFocusedCommit(key.Matches(msg, m.keys.Up))

		case key.Matches(msg, m.keys.Down):
			if currSection != nil {
				prevRow := currSection.CurrRow()
				nextRow := currSection.NextRow()
				if prevRow != nextRow && nextRow == currSection.NumRows()-1 &&
					m.ctx.View != config.RepoView {
					cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
				}
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.Up):
			if currSection != nil {
				currSection.PrevRow()
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.FirstLine):
			if currSection != nil {
				currSection.FirstItem()
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.LastLine):
			if currSection != nil {
				if currSection.CurrRow()+1 < currSection.NumRows() {
					cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
				}
				currSection.LastItem()
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.TogglePreview):
			m.sidebar.IsOpen = !m.sidebar.IsOpen
			m.syncMainContentDimensions()

		case key.Matches(msg, m.keys.TogglePreviewPosition):
			if m.sidebar.IsOpen {
				if m.ctx.PreviewPosition == "right" {
					m.positionOverride = "bottom"
				} else {
					m.positionOverride = "right"
				}
				m.syncMainContentDimensions()
				m.syncProgramContext()
				cmd := m.syncSidebar()
				cmds = append(cmds, cmd)
			}

		case key.Matches(msg, m.keys.Refresh):
			if currSection != nil {
				data.ClearEnrichmentCache()
				currSection.ResetFilters()
				currSection.ResetRows()
				m.syncSidebar()
				cmds = append(cmds, currSection.SetIsLoading(true))
				cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
			}

		case key.Matches(msg, m.keys.RefreshAll):
			data.ClearEnrichmentCache()
			fetchSectionsCmds := m.fetchAllViewSections()
			m.updateTabs()
			cmds = append(cmds, fetchSectionsCmds)

		case key.Matches(msg, m.keys.Redraw):
			// with bubbletea v2's declarative approach, if we just clear the screen then tea will redraw for us
			return m, tea.ClearScreen

		case key.Matches(msg, m.keys.Search):
			if currSection != nil {
				cmd = currSection.SetIsSearching(true)
				return m, cmd
			}

		case key.Matches(msg, m.keys.Help):
			m.footer.ShowAll = !m.footer.ShowAll
			m.syncMainContentDimensions()

		case key.Matches(msg, m.keys.CopyNumber):
			var cmd tea.Cmd
			if currRowData == nil || reflect.ValueOf(currRowData).IsNil() {
				cmd = m.notifyErr("Current selection isn't associated with a PR/Issue")
				return m, cmd
			}
			number := fmt.Sprint(currRowData.GetNumber())
			err := clipboard.WriteAll(number)
			if err != nil {
				cmd = m.notifyErr(fmt.Sprintf("Failed copying to clipboard %v", err))
			} else {
				cmd = m.notify(fmt.Sprintf("Copied %s to clipboard", number))
			}
			return m, cmd

		case key.Matches(msg, m.keys.CopyUrl):
			var cmd tea.Cmd
			if currRowData == nil || reflect.ValueOf(currRowData).IsNil() {
				cmd = m.notifyErr("Current selection isn't associated with a PR/Issue")
				return m, cmd
			}
			url := currRowData.GetUrl()
			err := clipboard.WriteAll(url)
			if err != nil {
				cmd = m.notifyErr(fmt.Sprintf("Failed copying to clipboard %v", err))
			} else {
				cmd = m.notify(fmt.Sprintf("Copied %s to clipboard", url))
			}
			return m, cmd

		case key.Matches(msg, m.keys.Quit):
			if !m.ctx.Config.ConfirmQuit {
				return m, tea.Quit
			}

			m.footer.SetShowConfirmQuit(true)

		case m.ctx.View == config.RepoView:
			switch {
			case key.Matches(msg, m.keys.OpenGithub):
				cmds = append(cmds, m.repo.(*reposection.Model).OpenGithub())

			case key.Matches(msg, keys.BranchKeys.Delete):
				if currSection != nil {
					currSection.SetPromptConfirmationAction("delete")
					cmd = currSection.SetIsPromptConfirmationShown(true)
				}
				return m, cmd

			case key.Matches(msg, keys.BranchKeys.New):
				if currSection != nil {
					currSection.SetPromptConfirmationAction("new")
					cmd = currSection.SetIsPromptConfirmationShown(true)
				}
				return m, cmd

			case key.Matches(msg, keys.BranchKeys.CreatePr):
				if currSection != nil {
					currSection.SetPromptConfirmationAction("create_pr")
					cmd = currSection.SetIsPromptConfirmationShown(true)
				}
				return m, cmd

			case key.Matches(msg, keys.BranchKeys.ViewPRs):
				cmds = append(cmds, m.switchSelectedView())
			}
		case m.ctx.View == config.PRsView:
			switch {
			case key.Matches(msg, keys.PRKeys.PrevSidebarTab),
				key.Matches(msg, keys.PRKeys.NextSidebarTab):
				var scmds []tea.Cmd
				var scmd tea.Cmd
				m.prView, scmd = m.prView.Update(msg)
				scmds = append(scmds, scmd)
				m.syncSidebar()
				return m, tea.Batch(scmds...)

			case key.Matches(msg, m.keys.OpenGithub):
				cmds = append(cmds, m.openBrowser())

			case key.Matches(msg, keys.PRKeys.Approve):
				return m, m.openSidebarForPRInput(m.prView.SetIsApproving)

			case key.Matches(msg, keys.PRKeys.Assign):
				return m, m.openSidebarForPRInput(m.prView.SetIsAssigning)

			case key.Matches(msg, keys.PRKeys.Unassign):
				return m, m.openSidebarForPRInput(m.prView.SetIsUnassigning)

			case key.Matches(msg, keys.PRKeys.Label):
				return m, m.openSidebarForPRInput(m.prView.SetIsLabeling)

			case key.Matches(msg, keys.PRKeys.Comment):
				return m, m.openSidebarForPRComment()

			case key.Matches(msg, keys.PRKeys.Close):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "close")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.Ready):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "ready")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.Reopen):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "reopen")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.Merge):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "merge")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.Update):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "update")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.ApproveWorkflows):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "approveWorkflows")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.ViewIssues):
				cmds = append(cmds, m.switchSelectedView())

			case key.Matches(msg, keys.PRKeys.SummaryViewMore):
				m.prView.SetSummaryViewMore()
				m.syncSidebar()
				return m, nil
			}
		case m.ctx.View == config.IssuesView:
			switch {
			case key.Matches(msg, m.keys.OpenGithub):
				cmds = append(cmds, m.openBrowser())

			case key.Matches(msg, keys.IssueKeys.Label):
				return m, m.openSidebarForInput(m.issueSidebar.SetIsLabeling)

			case key.Matches(msg, keys.IssueKeys.Assign):
				return m, m.openSidebarForInput(m.issueSidebar.SetIsAssigning)

			case key.Matches(msg, keys.IssueKeys.Unassign):
				return m, m.openSidebarForInput(m.issueSidebar.SetIsUnassigning)

			case key.Matches(msg, keys.IssueKeys.Comment):
				return m, m.openSidebarForIssueComment()

			case key.Matches(msg, keys.IssueKeys.Checkout):
				cmd, err := m.issueSidebar.Checkout()
				if err != nil {
					m.ctx.Error = err
				}
				return m, cmd

			case key.Matches(msg, keys.IssueKeys.Close):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "close")
				}
				return m, cmd

			case key.Matches(msg, keys.IssueKeys.Reopen):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "reopen")
				}
				return m, cmd

			case key.Matches(msg, keys.IssueKeys.ViewPRs):
				cmds = append(cmds, m.switchSelectedView())
			}
		case m.ctx.View == config.NotificationsView:
			switch {
			case key.Matches(msg, m.keys.OpenGithub):
				cmds = append(cmds, m.openBrowser())
				return m, tea.Batch(cmds...)

			// Handle Enter to (re)load notification content - check before subject handlers
			// so Enter always works, even after viewing a notification
			case key.Matches(msg, keys.NotificationKeys.View):
				cmds = append(cmds, m.loadNotificationContent())

			// Return from PR/Issue detail back to the default notification prompt
			case key.Matches(msg, keys.NotificationKeys.BackToNotification):
				return m, m.backToNotification()

			// Scrolling the preview only moves the sidebar's viewport (below), so
			// skip the subject handlers, which re-render the whole preview
			case (m.notificationView.GetSubjectPR() != nil ||
				m.notificationView.GetSubjectIssue() != nil) &&
				(key.Matches(msg, m.keys.PageDown) || key.Matches(msg, m.keys.PageUp)):

			// PR keybindings when viewing a PR notification
			case m.notificationView.GetSubjectPR() != nil:
				// Check for PR actions first (before updating prView)
				if !m.prView.IsTextInputBoxFocused() {
					action := prview.MsgToAction(msg)
					if action != nil {
						switch action.Type {
						case prview.PRActionApprove:
							return m, m.openSidebarForPRInput(m.prView.SetIsApproving)

						case prview.PRActionAssign:
							return m, m.openSidebarForPRInput(m.prView.SetIsAssigning)

						case prview.PRActionUnassign:
							return m, m.openSidebarForPRInput(m.prView.SetIsUnassigning)

						case prview.PRActionLabel:
							return m, m.openSidebarForPRInput(m.prView.SetIsLabeling)

						case prview.PRActionComment:
							return m, m.openSidebarForPRComment()

						case prview.PRActionDiff:
							if pr := m.notificationView.GetSubjectPR(); pr != nil {
								cmd = common.DiffPR(pr.GetNumber(), pr.GetRepoNameWithOwner(),
									m.ctx.Config.GetFullScreenDiffPagerEnv())
							}
							return m, cmd

						case prview.PRActionCheckout:
							if pr := m.notificationView.GetSubjectPR(); pr != nil {
								var err error
								cmd, err = notificationssection.CheckoutPR(
									m.ctx, pr.GetNumber(), pr.GetRepoNameWithOwner())
								if err != nil {
									m.ctx.Error = err
								}
							}
							return m, cmd

						case prview.PRActionClose:
							cmd = m.promptConfirmationForNotificationPR("close")
							return m, cmd

						case prview.PRActionReady:
							cmd = m.promptConfirmationForNotificationPR("ready")
							return m, cmd

						case prview.PRActionReopen:
							cmd = m.promptConfirmationForNotificationPR("reopen")
							return m, cmd

						case prview.PRActionMerge:
							cmd = m.promptConfirmationForNotificationPR("merge")
							return m, cmd

						case prview.PRActionUpdate:
							cmd = m.promptConfirmationForNotificationPR("update")
							return m, cmd

						case prview.PRActionApproveWorkflows:
							cmd = m.promptConfirmationForNotificationPR("approveWorkflows")
							return m, cmd

						case prview.PRActionSummaryViewMore:
							m.prView.SetSummaryViewMore()
							m.syncSidebar()
							return m, nil
						}
					}
				}

				// Handle 's' key to switch views
				if key.Matches(msg, keys.PRKeys.ViewIssues) {
					cmds = append(cmds, m.switchSelectedView())
				}

				// No action matched - update prView for navigation (tab switching, scrolling)
				var prCmd tea.Cmd
				m.prView, prCmd = m.prView.Update(msg)
				m.syncSidebar()
				cmds = append(cmds, prCmd)

			// Issue keybindings when viewing an Issue notification
			case m.notificationView.GetSubjectIssue() != nil:
				var issueCmd tea.Cmd
				var action *issueview.IssueAction
				m.issueSidebar, issueCmd, action = m.issueSidebar.Update(msg)

				if action != nil {
					switch action.Type {
					case issueview.IssueActionLabel:
						return m, m.openSidebarForInput(m.issueSidebar.SetIsLabeling)

					case issueview.IssueActionAssign:
						return m, m.openSidebarForInput(m.issueSidebar.SetIsAssigning)

					case issueview.IssueActionUnassign:
						return m, m.openSidebarForInput(m.issueSidebar.SetIsUnassigning)

					case issueview.IssueActionComment:
						return m, m.openSidebarForIssueComment()

					case issueview.IssueActionCheckout:
						cmd, err := m.issueSidebar.Checkout()
						if err != nil {
							m.ctx.Error = err
						}
						return m, cmd

					case issueview.IssueActionClose:
						cmd = m.promptConfirmationForNotificationIssue("close")
						return m, cmd

					case issueview.IssueActionReopen:
						cmd = m.promptConfirmationForNotificationIssue("reopen")
						return m, cmd
					}
				}

				// Handle 's' key to switch views
				if key.Matches(msg, keys.IssueKeys.ViewPRs) {
					cmds = append(cmds, m.switchSelectedView())
				}

				// Sync sidebar and return issueCmd for navigation
				m.syncSidebar()
				cmds = append(cmds, issueCmd)

			case key.Matches(msg, keys.NotificationKeys.MarkAsDone):
				cmds = append(
					cmds,
					m.updateSection(currSection.GetId(), currSection.GetType(), msg),
				)

			case key.Matches(msg, keys.NotificationKeys.MarkAllAsDone):
				cmd = m.promptConfirmation(currSection, "done_all")
				return m, cmd

			case key.Matches(msg, keys.NotificationKeys.Open):
				cmd = m.updateSection(currSection.GetId(), currSection.GetType(), msg)
				return m, cmd

			case key.Matches(msg, keys.NotificationKeys.SortByRepo):
				cmd = m.updateSection(currSection.GetId(), currSection.GetType(), msg)
				return m, cmd

			case key.Matches(msg, keys.PRKeys.ViewIssues):
				cmds = append(cmds, m.switchSelectedView())
			}
		}

	case initMsg:
		m.ctx.Config = &msg.Config
		m.ctx.RepoUrl = msg.RepoUrl
		m.ctx.Theme = theme.ParseTheme(m.ctx.Config)
		m.ctx.Styles = context.InitStyles(m.ctx.Theme)
		m.taskSpinner.Style = lipgloss.NewStyle().
			Background(m.ctx.Theme.SelectedBackground)

		m.ctx.View = m.ctx.Config.Defaults.View
		m.currSectionId = 0
		m.sidebar.IsOpen = msg.Config.Defaults.Preview.Open
		m.syncMainContentDimensions()

		m.initSections()
		fetchSectionsCmds := m.fetchAllViewSections()
		m.updateTabs()
		m.tabs.SetCurrSectionId(0)

		if m.ctx.BackgroundSource != "bubbletea" {
			log.Debugf("Setting markdownStyle in initMsg")
			m.ctx.HasDarkBackground = compat.HasDarkBackground
			m.ctx.BackgroundSource = "compat"
			log.Debugf(
				"HasDarkBackground: %t, BackgroundSource: %s",
				m.ctx.HasDarkBackground,
				m.ctx.BackgroundSource,
			)
			markdown.InitializeMarkdownStyle(m.ctx)
		}

		cmds = append(cmds, fetchSectionsCmds, m.tabs.Init(), fetchUser,
			m.doRefreshAtInterval(), m.doUpdateFooterAtInterval())

	case intervalRefresh:
		fetchSectionsCmds := m.fetchAllViewSections()
		m.updateTabs()
		cmds = append(cmds, fetchSectionsCmds, m.doRefreshAtInterval())

	case prview.CommitFilesMsg:
		if msg.Err != nil {
			log.Error("failed fetching commit files", "oid", msg.Oid, "err", msg.Err)
		}
		m.prView.SetCommitFiles(msg)
		m.syncSidebar()

	case userFetchedMsg:
		m.ctx.User = msg.user

	case constants.TaskFinishedMsg:
		task, ok := m.tasks[msg.TaskId]
		if ok {
			log.Info("Task finished", "id", task.Id)
			if msg.Err != nil {
				log.Error("Task finished with error", "id", task.Id, "err", msg.Err)
				task.State = context.TaskError
				task.Error = msg.Err
			} else {
				task.State = context.TaskFinished
			}
			now := time.Now()
			task.FinishedTime = &now
			m.tasks[msg.TaskId] = task
			clear := tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return constants.ClearTaskMsg{TaskId: msg.TaskId}
			})
			cmds = append(cmds, clear)

			scmd := m.updateSection(msg.SectionId, msg.SectionType, msg.Msg)
			cmds = append(cmds, scmd)
			m.updateNotificationSubject(msg.Msg)

			syncCmd := m.syncSidebar()
			cmds = append(cmds, syncCmd)
		}

	case prview.EnrichedPrMsg:
		if msg.Err == nil {
			m.prView.SetEnrichedPR(msg.Data)
			m.prs[msg.Id].(*prssection.Model).EnrichPR(msg.Data)
			syncCmd := m.syncSidebar()
			cmds = append(cmds, syncCmd)
		} else {
			log.Error("failed enriching pr", "err", msg.Err)
		}

	case notificationPRFetchedMsg:
		m.stopNotificationLoading(msg.NotificationId)
		if msg.Err == nil {
			// Convert enriched PR to prrow.Data for display
			prData := msg.PR.ToPullRequestData()
			m.notificationView.SetSubjectPR(&prrow.Data{
				Primary:    &prData,
				Enriched:   msg.PR,
				IsEnriched: true,
			}, msg.NotificationId)
			keys.SetNotificationSubject(keys.NotificationSubjectPR)
			// Update sidebar with PR view
			width := m.sidebar.GetSidebarContentWidth()
			m.prView.SetSectionId(0)
			m.prView.SetRow(m.notificationView.GetSubjectPR())
			m.prView.SetWidth(width)
			m.prView.SetEnrichedPR(msg.PR)
			m.restoreDraft(msg.NotificationId, true)
			// Switch to Activity tab and scroll to bottom if there's a latest comment
			// (indicates there's new activity to show)
			if msg.LatestCommentUrl != "" {
				m.prView.GoToActivityTab()
				m.setSidebarPRContent()
				m.sidebar.ScrollToBottom()
			} else {
				// For notifications without comments (new PRs, state changes, etc.)
				// show the Overview tab without scrolling
				m.prView.GoToFirstTab()
				m.setSidebarPRContent()
			}
			m.markNotificationAsRead(msg.NotificationId)
		} else {
			log.Error("failed fetching notification PR", "err", msg.Err)
			m.ctx.Error = msg.Err
		}

	case notificationIssueFetchedMsg:
		m.stopNotificationLoading(msg.NotificationId)
		if msg.Err == nil {
			m.notificationView.SetSubjectIssue(&msg.Issue, msg.NotificationId)
			keys.SetNotificationSubject(keys.NotificationSubjectIssue)
			// Update sidebar with Issue view
			width := m.sidebar.GetSidebarContentWidth()
			m.issueSidebar.SetSectionId(0)
			m.issueSidebar.SetRow(m.notificationView.GetSubjectIssue())
			m.issueSidebar.SetWidth(width)
			m.restoreDraft(msg.NotificationId, false)
			m.setSidebarIssueContent()
			// Scroll to bottom if there's a latest comment (indicates new activity)
			if msg.LatestCommentUrl != "" {
				m.sidebar.ScrollToBottom()
			}
			m.markNotificationAsRead(msg.NotificationId)
		} else {
			log.Error("failed fetching notification Issue", "err", msg.Err)
			m.ctx.Error = msg.Err
		}

	case notificationssection.UpdateNotificationReadStateMsg:
		m.updateNotificationSections(msg)

	case notificationssection.UpdateNotificationCommentsMsg:
		cmds = append(cmds, m.updateNotificationSections(msg))

	case notificationssection.NotificationUpdatesBatchMsg:
		for _, update := range msg.Updates {
			cmds = append(cmds, m.updateNotificationSections(update))
		}
		// Wait for the next batch once here, not once per section
		cmds = append(cmds, msg.Next())

	case spinner.TickMsg:
		if m.notificationView.IsLoading() {
			cmds = append(cmds, m.notificationView.UpdateLoadingSpinner(msg))
			if m.isNotificationLoadingShown() {
				m.sidebar.SetContent(m.notificationView.View())
			}
		}
		if len(m.tasks) > 0 {
			taskSpinner, internalTickCmd := m.taskSpinner.Update(msg)
			m.taskSpinner = taskSpinner
			rTask := m.renderRunningTask()
			m.footer.SetRightSection(rTask)
			cmd = internalTickCmd
		}

	case constants.ClearTaskMsg:
		m.footer.SetRightSection("")
		delete(m.tasks, msg.TaskId)

	case section.SectionMsg:
		cmd = m.updateRelevantSection(msg)

		if msg.Id == m.currSectionId {
			cmds = append(cmds, m.onViewedRowChanged())
		}

	case execProcessFinishedMsg, tea.FocusMsg:
		if currSection != nil {
			cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
		}
		// The color scheme may have changed while we weren't looking
		cmds = append(cmds, tea.RequestBackgroundColor)

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if zone.Get("donate").InBounds(msg) {
			log.Info("Donate clicked", "msg", msg)
			openCmd := func() tea.Msg {
				// Discard the launcher's stdout/stderr so any noise (e.g.
				// GTK / GVFS warnings from xdg-open / gnome-open) does not
				// leak into the TUI's terminal and corrupt the display.
				// See #829, #584, #679.
				b := browser.New("", io.Discard, io.Discard)
				err := b.Browse("https://github.com/sponsors/dlvhdr")
				if err != nil {
					return constants.ErrMsg{Err: err}
				}
				return nil
			}
			cmds = append(cmds, openCmd)
		}

	case tea.WindowSizeMsg:
		m.onWindowSizeChanged(msg)

	case tea.ModeReportMsg:
		log.Debug("Mode report", "mode", msg.Mode, "value", msg.Value)
		if msg.Mode == ansi.ModeLightDark && msg.Value.IsNotRecognized() {
			log.Debug("Terminal doesn't report color scheme changes, polling background color")
			cmds = append(cmds, pollBackgroundColor())
		}

	case pollBackgroundColorMsg:
		cmds = append(cmds, tea.RequestBackgroundColor, pollBackgroundColor())

	case uv.DarkColorSchemeEvent, uv.LightColorSchemeEvent:
		// The terminal's color scheme changed; re-query the actual background
		// color and let the BackgroundColorMsg handler restyle everything.
		cmds = append(cmds, tea.RequestBackgroundColor)

	case tea.BackgroundColorMsg:
		if m.ctx.BackgroundSource == "bubbletea" &&
			m.ctx.HasDarkBackground == msg.IsDark() {
			// Most likely a poll that found nothing changed
			break
		}
		log.Debugf("Setting markdownStyle in BackgroundColorMsg %s", msg.String())
		m.ctx.HasDarkBackground = msg.IsDark()
		m.ctx.BackgroundSource = "bubbletea"
		// Adaptive colors read this global at render time.
		compat.HasDarkBackground = m.ctx.HasDarkBackground
		log.Debugf(
			"HasDarkBackground: %t, BackgroundSource: %s",
			m.ctx.HasDarkBackground,
			m.ctx.BackgroundSource,
		)
		markdown.InitializeMarkdownStyle(m.ctx)
		if m.ctx.Config != nil {
			// Some styles are rendered to strings up front, e.g. glyphs
			m.ctx.Styles = context.InitStyles(m.ctx.Theme)
			m.rebuildAllSectionRows()
			cmds = append(cmds, m.syncSidebar())
		}

	case updateFooterMsg:
		cmds = append(cmds, cmd, m.doUpdateFooterAtInterval())

	case constants.ErrMsg:
		m.ctx.Error = msg.Err
	}

	m.syncProgramContext()

	var bsCmd tea.Cmd
	m.branchSidebar, bsCmd = m.branchSidebar.Update(msg)
	cmds = append(cmds, bsCmd)

	m.sidebar, sidebarCmd = m.sidebar.Update(msg)

	if m.prView.IsTextInputBoxFocused() {
		m.prView, prViewCmd = m.prView.Update(msg)
		m.syncSidebar()
	}

	if m.issueSidebar.IsTextInputBoxFocused() {
		m.issueSidebar, issueSidebarCmd, _ = m.issueSidebar.Update(msg)
		m.syncSidebar()
	}

	if currSection != nil {
		if currSection.IsPromptConfirmationFocused() {
			m.footer.SetLeftSection(currSection.GetPromptConfirmation())
		}

		if !currSection.IsPromptConfirmationFocused() {
			m.footer.SetLeftSection(currSection.GetPagerContent())
		}
	}

	tm, tabsCmd := m.tabs.Update(msg)
	m.tabs = tm

	sectionCmd := m.updateCurrentSection(msg)
	cmds = append(
		cmds,
		cmd,
		tabsCmd,
		sidebarCmd,
		footerCmd,
		sectionCmd,
		prViewCmd,
		issueSidebarCmd,
	)

	return m, tea.Batch(cmds...)
}

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeCellMotion

	if m.ctx.Config == nil {
		v.Content = lipgloss.Place(
			m.ctx.ScreenWidth,
			m.ctx.ScreenHeight,
			lipgloss.Center,
			lipgloss.Center,
			"Reading config...",
		)
		return v
	}

	s := strings.Builder{}
	if m.ctx.View != config.RepoView {
		s.WriteString(m.tabs.View())
	}
	s.WriteString("\n")
	m.updateSidebarHints()
	var content string
	currSection := m.getCurrSection()
	if currSection != nil {
		if m.ctx.PreviewPosition == "bottom" && m.sidebar.IsOpen {
			content = lipgloss.JoinVertical(
				lipgloss.Left,
				m.getCurrSection().View(),
				m.sidebar.View(),
			)
		} else {
			content = lipgloss.JoinHorizontal(
				lipgloss.Top,
				m.getCurrSection().View(),
				m.sidebar.View(),
			)
		}
	} else {
		content = lipgloss.Place(
			m.ctx.MainContentWidth,
			m.ctx.MainContentHeight,
			lipgloss.Center,
			lipgloss.Center,
			"No sections defined",
		)
	}
	s.WriteString(content)
	s.WriteString("\n")
	if m.ctx.Error != nil {
		s.WriteString(
			m.ctx.Styles.Common.ErrorStyle.
				Width(m.ctx.ScreenWidth).
				Render(fmt.Sprintf("%s %s",
					m.ctx.Styles.Common.FailureGlyph,
					lipgloss.NewStyle().
						Foreground(m.ctx.Theme.ErrorText).
						Render(m.ctx.Error.Error()),
				)),
		)
	} else {
		s.WriteString(m.footer.View())
	}

	base := zone.Scan(s.String())
	layers := []*lipgloss.Layer{
		lipgloss.NewLayer(base),
	}

	if currSection != nil {
		searchCmp := currSection.ViewCompletions()
		if searchCmp != "" {
			y := common.HeaderHeight + common.SearchHeight + 1
			layers = append(layers, lipgloss.NewLayer(searchCmp).X(1).Y(y))
		}
	}

	prCmp := m.prView.ViewCompletions()
	previewPos := m.ctx.PreviewCursorPosition()
	if prCmp != "" {
		y := m.ctx.ScreenHeight - common.FooterHeight - m.prView.InputBoxLineFromBottom() - common.InputBoxHeight - 6
		layers = append(layers, lipgloss.NewLayer(prCmp).X(previewPos.X+3).Y(y))
	}

	issueCmp := m.issueSidebar.ViewCompletions()
	if issueCmp != "" {
		y := m.ctx.ScreenHeight - common.FooterHeight - m.issueSidebar.InputBoxLineFromButton() - common.InputBoxHeight - 6
		layers = append(layers, lipgloss.NewLayer(issueCmp).X(previewPos.X+3).Y(y))
	}

	// Compositing re-parses the whole screen, so only pay for it when there
	// are popups to overlay on top of the base layer.
	if len(layers) == 1 {
		v.SetContent(base)
		return v
	}

	comp := lipgloss.NewCompositor(layers...)
	v.SetContent(comp.Render())

	return v
}

type initMsg struct {
	Config  config.Config
	RepoUrl string
}

// Message types for notification subject fetching
type notificationPRFetchedMsg struct {
	NotificationId   string
	PR               data.EnrichedPullRequestData
	LatestCommentUrl string
	Err              error
}

// notificationSubjectRefreshedMsg carries a refetched PR or Issue of an open
// notification
type notificationSubjectRefreshedMsg struct {
	NotificationId string
	PR             *data.EnrichedPullRequestData
	Issue          *data.IssueData
}

type notificationIssueFetchedMsg struct {
	NotificationId   string
	Issue            data.IssueData
	LatestCommentUrl string
	Err              error
}

// stopNotificationLoading stops the notification view's loading spinner once
// the subject it was loading has arrived. The sidebar is re-rendered so it no
// longer shows the spinner, e.g. when fetching failed.
func (m *Model) stopNotificationLoading(notificationId string) {
	if m.notificationView.GetLoadingId() != notificationId {
		return
	}
	shown := m.isNotificationLoadingShown()
	m.notificationView.StopLoading()
	if shown {
		m.sidebar.SetContent(m.notificationView.View())
	}
}

// isNotificationLoadingShown reports whether the sidebar is showing the
// notification whose subject is being loaded.
func (m *Model) isNotificationLoadingShown() bool {
	if !m.sidebar.IsOpen || m.ctx.View != config.NotificationsView {
		return false
	}
	row, ok := m.getCurrRowData().(*notificationrow.Data)
	return ok && row != nil && row.GetId() == m.notificationView.GetLoadingId()
}

// resumeLoadingSpinner restarts the current section's loading spinner if it's
// still loading. Spinner ticks only go to the current section, so a section
// that was loading in the background has a stopped spinner.
func (m *Model) resumeLoadingSpinner() tea.Cmd {
	currSection := m.getCurrSection()
	if currSection == nil || !currSection.GetIsLoading() {
		return nil
	}
	return currSection.SetIsLoading(true)
}

func (m *Model) setCurrSectionId(newSectionId int) {
	m.currSectionId = newSectionId
	m.tabs.SetCurrSectionId(newSectionId)
}

func (m *Model) updateNotificationSections(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.notifications {
		if m.notifications[i] != nil {
			var cmd tea.Cmd
			m.notifications[i], cmd = m.notifications[i].Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) markNotificationAsRead(notificationId string) {
	readStateMsg := notificationssection.UpdateNotificationReadStateMsg{
		Id:     notificationId,
		Unread: false,
	}
	m.updateNotificationSections(readStateMsg)
}

func (m *Model) onViewedRowChanged() tea.Cmd {
	m.prView.SetSummaryViewLess()
	m.prView.GoToFirstTab()
	sidebarCmd := m.syncSidebar()
	enrichCmd := m.prView.EnrichCurrRow()
	m.sidebar.ScrollToTop()
	m.stashDraft()
	m.notificationView.ResetSubject()
	keys.SetNotificationSubject(keys.NotificationSubjectNone)
	return tea.Batch(sidebarCmd, enrichCmd)
}

func (m *Model) onWindowSizeChanged(msg tea.WindowSizeMsg) {
	log.Info("window size changed", "width", msg.Width, "height", msg.Height)
	m.footer.SetWidth(msg.Width)
	m.ctx.ScreenWidth = msg.Width
	m.ctx.ScreenHeight = msg.Height
	if m.ctx.Config != nil {
		if m.ctx.Config.Defaults.Preview.Position == "auto" ||
			m.ctx.Config.Defaults.Preview.Position == "" {
			m.positionOverride = ""
		}
		m.syncMainContentDimensions()
		m.syncSidebar()
	}
}

func (m *Model) syncProgramContext() {
	for _, section := range m.getCurrentViewSections() {
		section.UpdateProgramContext(m.ctx)
	}
	m.tabs.UpdateProgramContext(m.ctx)
	m.footer.UpdateProgramContext(m.ctx)
	m.sidebar.UpdateProgramContext(m.ctx)
	m.prView.UpdateProgramContext(m.ctx)
	m.issueSidebar.UpdateProgramContext(m.ctx)
	m.branchSidebar.UpdateProgramContext(m.ctx)
	m.notificationView.UpdateProgramContext(m.ctx)
}

func (m *Model) updateSection(id int, sType string, msg tea.Msg) (cmd tea.Cmd) {
	var updatedSection section.Section
	switch sType {
	case reposection.SectionType:
		m.repo, cmd = m.repo.Update(msg)

	case notificationssection.SectionType:
		if id < len(m.notifications) && m.notifications[id] != nil {
			m.notifications[id], cmd = m.notifications[id].Update(msg)
		}

	case prssection.SectionType:
		updatedSection, cmd = m.prs[id].Update(msg)
		m.prs[id] = updatedSection
	case issuessection.SectionType:
		updatedSection, cmd = m.issues[id].Update(msg)
		m.issues[id] = updatedSection
	}

	currSection := m.getCurrSection()
	if currSection != nil && id == currSection.GetId() {
		if _, ok := msg.(prssection.SectionPullRequestsFetchedMsg); ok {
			cmd = m.onViewedRowChanged()
		}
	}

	return cmd
}

func (m *Model) updateRelevantSection(msg section.SectionMsg) (cmd tea.Cmd) {
	return m.updateSection(msg.Id, msg.Type, msg)
}

func (m *Model) updateCurrentSection(msg tea.Msg) (cmd tea.Cmd) {
	section := m.getCurrSection()
	if section == nil {
		return nil
	}
	return m.updateSection(section.GetId(), section.GetType(), msg)
}

const minTableWidthForRightPreview = 80

func (m *Model) resolvePreviewPosition() string {
	pos := m.ctx.Config.Defaults.Preview.Position
	if pos == "" {
		pos = "auto"
	}

	if m.positionOverride != "" {
		return m.positionOverride
	}

	if pos == "right" || pos == "bottom" {
		return pos
	}

	// auto: check if right mode would leave enough room for the main content
	w := m.ctx.Config.Defaults.Preview.Width
	if w > 0 && w < 1 {
		w *= float64(m.ctx.ScreenWidth)
	}
	previewWidth := min(int(w), m.ctx.ScreenWidth)
	tableWidth := m.ctx.ScreenWidth - previewWidth
	if tableWidth < minTableWidthForRightPreview {
		return "bottom"
	}
	return "right"
}

func (m *Model) getBaseContentHeight() int {
	if m.footer.ShowAll {
		// Measure actual footer height — the ExpandedHelpHeight constant
		// doesn't account for custom keybindings or view-specific bindings.
		footerHeight := lipgloss.Height(m.footer.View())
		return m.ctx.ScreenHeight - common.TabsHeight - footerHeight
	}
	return m.ctx.ScreenHeight - common.TabsHeight - common.FooterHeight
}

func (m *Model) syncMainContentDimensions() {
	m.ctx.PreviewPosition = m.resolvePreviewPosition()

	if !m.sidebar.IsOpen {
		m.ctx.MainContentWidth = m.ctx.ScreenWidth
		m.ctx.MainContentHeight = m.getBaseContentHeight()
		m.ctx.DynamicPreviewWidth = 0
		m.ctx.DynamicPreviewHeight = 0
		m.ctx.SidebarOpen = false
		return
	}

	m.ctx.SidebarOpen = true

	if m.ctx.PreviewPosition == "bottom" {
		m.ctx.MainContentWidth = m.ctx.ScreenWidth

		// Subtract border height: lipgloss Height() sets content height,
		// and BorderTop adds an extra row outside of that.
		availableHeight := m.getBaseContentHeight() - m.ctx.Styles.Sidebar.BorderWidth
		h := m.ctx.Config.Defaults.Preview.Height
		if h > 0 && h < 1 {
			h *= float64(availableHeight)
		}
		m.ctx.DynamicPreviewHeight = min(int(h), availableHeight)
		m.ctx.MainContentHeight = availableHeight - m.ctx.DynamicPreviewHeight
		m.ctx.DynamicPreviewWidth = m.ctx.ScreenWidth
	} else {
		m.ctx.MainContentHeight = m.getBaseContentHeight()

		w := m.ctx.Config.Defaults.Preview.Width
		if w > 0 && w < 1 {
			w *= float64(m.ctx.ScreenWidth)
		}
		m.ctx.DynamicPreviewWidth = min(int(w), m.ctx.ScreenWidth)
		m.ctx.MainContentWidth = m.ctx.ScreenWidth - m.ctx.DynamicPreviewWidth
		m.ctx.DynamicPreviewHeight = 0
	}
}

func (m *Model) openSidebarForPRInput(setFunc func(bool) tea.Cmd) tea.Cmd {
	m.prView.GoToFirstTab()
	return m.openSidebarForInput(setFunc)
}

// openSidebarForPRComment opens the comment editor below the PR's activity,
// scrolled to the latest comments, or continues a detached draft.
func (m *Model) openSidebarForPRComment() tea.Cmd {
	if m.hasDetachedDraft() {
		return m.continueDraft()
	}
	m.prView.GoToActivityTab()
	cmd := m.openSidebarForInput(func(bool) tea.Cmd {
		return m.prView.StartComment("", m.isNotificationSubjectShown())
	})
	m.sidebar.ScrollToBottom()
	return cmd
}

// openSidebarForIssueComment opens the comment editor below the issue's
// comments, or continues a detached draft.
func (m *Model) openSidebarForIssueComment() tea.Cmd {
	if m.hasDetachedDraft() {
		return m.continueDraft()
	}
	cmd := m.openSidebarForInput(func(bool) tea.Cmd {
		return m.issueSidebar.StartComment("", m.isNotificationSubjectShown())
	})
	m.sidebar.ScrollToBottom()
	return cmd
}

func (m *Model) openSidebarForInput(setFunc func(bool) tea.Cmd) tea.Cmd {
	m.sidebar.IsOpen = true
	cmd := setFunc(true)
	m.syncMainContentDimensions()
	m.syncSidebar()
	return cmd
}

// updateSidebarHints sets the key hints the sidebar shows for moving around
// and acting on what's focused in it.
func (m *Model) updateSidebarHints() {
	m.sidebar.SetNavKeysScroll(m.isNotificationSubjectShown())
	switch {
	case m.isNotificationSubjectShown() && m.notificationView.GetSubjectPR() != nil &&
		m.prView.IsCommitsTab():
		m.sidebar.SetFocusHint(keys.HintKeys(keys.NotificationKeys.ViewCommitFiles) + " files")
		m.sidebar.SetFocusLabel("commit")
	case m.isNotificationSubjectShown() && m.notificationView.GetSubjectPR() != nil &&
		m.prView.IsChecksTab():
		m.sidebar.SetFocusHint(keys.HintKeys(keys.NotificationKeys.OpenCheck) + " open")
		m.sidebar.SetFocusLabel("check")
	case m.isNotificationSubjectShown():
		m.sidebar.SetFocusHint(keys.HintKeys(keys.NotificationKeys.ReplyToComment) + " reply")
		m.sidebar.SetFocusLabel("comment")
	default:
		m.sidebar.SetFocusHint("")
		m.sidebar.SetFocusLabel("")
	}
}

// previewScrollLines is how far the navigation keys scroll an open
// notification's preview.
const previewScrollLines = 3

// isNotificationSubjectShown reports whether a notification's PR or Issue is
// open in the preview.
func (m *Model) isNotificationSubjectShown() bool {
	return m.ctx.View == config.NotificationsView && m.sidebar.IsOpen &&
		(m.notificationView.GetSubjectPR() != nil || m.notificationView.GetSubjectIssue() != nil)
}

func (m *Model) backToNotification() tea.Cmd {
	if m.notificationView.GetSubjectPR() == nil && m.notificationView.GetSubjectIssue() == nil {
		return nil
	}

	m.stashDraft()
	m.notificationView.ClearSubject()
	keys.SetNotificationSubject(keys.NotificationSubjectNone)
	m.sidebar.ClearSearch()
	m.sidebar.ScrollToTop()
	return m.syncSidebar()
}

func (m *Model) promptConfirmation(currSection section.Section, action string) tea.Cmd {
	if currSection != nil {
		currSection.SetPromptConfirmationAction(action)
		return currSection.SetIsPromptConfirmationShown(true)
	}
	return nil
}

func (m *Model) setSidebarPRContent() {
	m.renderSidebarPRContent()
	m.syncFocusedCommit(false)
}

func (m *Model) renderSidebarPRContent() {
	body, comments := m.prView.ViewBodyWithAnchors()
	m.setSidebarContentWithComments(m.prView.ViewHeader(), body,
		m.prView.ViewEditor(m.draftHint()), comments)
}

func (m *Model) setSidebarIssueContent() {
	body, comments := m.issueSidebar.ViewBodyWithAnchors()
	m.setSidebarContentWithComments(m.issueSidebar.ViewHeader(), body,
		m.issueSidebar.ViewEditor(m.draftHint()), comments)
}

func (m *Model) setSidebarContentWithComments(
	header, body, editor string,
	comments []common.CommentAnchor,
) {
	m.sidebarComments = comments
	lines := make([]int, 0, len(comments))
	for _, c := range comments {
		lines = append(lines, c.Line)
	}
	m.sidebar.SetActionHints(m.notificationActionHints())
	m.sidebar.SetContentWithHeader(header, body, editor, lines)
}

// notificationActionHints lists the keys for acting on an open notification
// and its PR/Issue, shown above it, most important first. A notification
// action whose key is taken by the PR/Issue isn't listed as it wouldn't work.
func (m *Model) notificationActionHints() []sidebar.ActionHint {
	if !m.isNotificationSubjectShown() {
		return nil
	}

	var subjectKeys, subjectActions []key.Binding
	if pr := m.notificationView.GetSubjectPR(); pr != nil {
		subjectKeys = append(keys.PRFullHelp(), keys.CustomPRBindings...)
		subjectActions = []key.Binding{keys.PRKeys.Comment, keys.PRKeys.Approve, keys.PRKeys.Diff,
			keys.PRKeys.Checkout}
		if pr.Primary != nil && pr.Primary.State == "OPEN" {
			subjectActions = append(subjectActions, keys.PRKeys.Merge)
		}
	} else if issue := m.notificationView.GetSubjectIssue(); issue != nil {
		subjectKeys = append(keys.IssueFullHelp(), keys.CustomIssueBindings...)
		subjectActions = []key.Binding{keys.IssueKeys.Comment, keys.IssueKeys.Label,
			keys.IssueKeys.Assign}
		if issue.State == "OPEN" {
			subjectActions = append(subjectActions, keys.IssueKeys.Close)
		} else {
			subjectActions = append(subjectActions, keys.IssueKeys.Reopen)
		}
	}

	taken := func(b key.Binding) bool {
		for _, k := range b.Keys() {
			for _, sk := range subjectKeys {
				if slices.Contains(sk.Keys(), k) {
					return true
				}
			}
		}
		return false
	}

	var hints []sidebar.ActionHint
	notificationActions := []struct {
		binding key.Binding
		label   string
	}{
		{keys.NotificationKeys.MarkAsDone, "done"},
		{keys.NotificationKeys.Unsubscribe, "unsubscribe"},
		{keys.NotificationKeys.ToggleBookmark, "bookmark"},
	}
	for _, a := range notificationActions {
		if a.binding.Enabled() && !taken(a.binding) {
			hints = append(hints, sidebar.ActionHint{Key: keys.HintKeys(a.binding), Label: a.label})
		}
	}
	for _, b := range subjectActions {
		hints = append(hints, sidebar.ActionHint{Key: keys.HintKeys(b), Label: b.Help().Desc})
	}
	return hints
}

// draftHint is shown on a detached comment draft.
func (m *Model) draftHint() string {
	if m.confirmingDraftDiscard {
		return "discard it? y/n"
	}
	return keys.HintKeys(keys.NotificationKeys.ContinueDraft) + " continue · " +
		keys.HintKeys(keys.NotificationKeys.DiscardDraft) + " discard"
}

// hasDetachedDraft reports whether an open notification has a comment draft
// that was detached from to read the preview.
func (m *Model) hasDetachedDraft() bool {
	if !m.isNotificationSubjectShown() {
		return false
	}
	if m.notificationView.GetSubjectPR() != nil {
		return m.prView.HasDetachedDraft()
	}
	return m.issueSidebar.HasDetachedDraft()
}

func (m *Model) continueDraft() tea.Cmd {
	var cmd tea.Cmd
	if m.notificationView.GetSubjectPR() != nil {
		cmd = m.prView.AttachDraft()
	} else {
		cmd = m.issueSidebar.AttachDraft()
	}
	m.syncSidebar()
	return cmd
}

func (m *Model) discardDraft() {
	if m.notificationView.GetSubjectPR() != nil {
		m.prView.DiscardEditor()
	} else {
		m.issueSidebar.DiscardEditor()
	}
	m.syncSidebar()
}

// stashDraft keeps the open notification's unsent comment, if any, so it can
// be restored when the notification is opened again, and closes the editor.
func (m *Model) stashDraft() {
	id := m.notificationView.GetSubjectId()
	if id == "" {
		return
	}
	var draft string
	if m.notificationView.GetSubjectPR() != nil {
		draft = m.prView.DraftValue()
		m.prView.DiscardEditor()
	} else if m.notificationView.GetSubjectIssue() != nil {
		draft = m.issueSidebar.DraftValue()
		m.issueSidebar.DiscardEditor()
	}
	m.confirmingDraftDiscard = false
	if strings.TrimSpace(draft) == "" {
		return
	}
	if m.drafts == nil {
		m.drafts = map[string]string{}
	}
	m.drafts[id] = draft
	m.updateNotificationSections(notificationssection.UpdateNotificationDraftMsg{Id: id, HasDraft: true})
}

// restoreDraft brings back a notification's stashed comment, detached so it's
// docked below the preview until continued.
func (m *Model) restoreDraft(notificationId string, isPR bool) {
	draft, ok := m.drafts[notificationId]
	if !ok {
		return
	}
	delete(m.drafts, notificationId)
	if isPR {
		m.prView.StartComment(draft, true)
		m.prView.DetachDraft()
	} else {
		m.issueSidebar.StartComment(draft, true)
		m.issueSidebar.DetachDraft()
	}
	m.updateNotificationSections(
		notificationssection.UpdateNotificationDraftMsg{Id: notificationId, HasDraft: false})
}

// focusedComment returns the comment focused in an open notification's
// preview, if any.
func (m *Model) focusedComment() (common.CommentAnchor, bool) {
	if !m.isNotificationSubjectShown() {
		return common.CommentAnchor{}, false
	}
	i := m.sidebar.FocusedAnchor()
	if i < 0 || i >= len(m.sidebarComments) || !m.sidebarComments[i].IsComment() {
		return common.CommentAnchor{}, false
	}
	return m.sidebarComments[i], true
}

// focusedCommit returns the index of the commit focused in an open
// notification's PR, if any.
func (m *Model) focusedCommit() (int, bool) {
	if !m.isNotificationSubjectShown() || m.notificationView.GetSubjectPR() == nil {
		return 0, false
	}
	i := m.sidebar.FocusedAnchor()
	if i < 0 || i >= len(m.sidebarComments) || m.sidebarComments[i].Commit == nil {
		return 0, false
	}
	return *m.sidebarComments[i].Commit, true
}

func (m *Model) hasFocusedCommit() bool {
	_, ok := m.focusedCommit()
	return ok
}

// syncFocusedCommit keeps a commit focused in an open notification's PR and
// shows its full message. When the focus is lost, e.g. on entering the tab or
// moving above the first commit, the expanded commit is focused again, e.g.
// coming back from its files, or else the first commit in view. Expanding a
// commit changes where the commits after it start, so the focus is put back
// on it once they're laid out anew. fromBelow is whether the focus moved up,
// to show a commit too tall for the view from its bottom.
func (m *Model) syncFocusedCommit(fromBelow bool) {
	if m.prView.IsChecksTab() {
		m.syncFocusedCheck(fromBelow)
		return
	}
	if !m.isNotificationSubjectShown() || m.notificationView.GetSubjectPR() == nil ||
		!m.prView.IsCommitsTab() {
		return
	}
	commit, ok := m.focusedCommit()
	if !ok {
		if anchor := m.commitAnchor(m.prView.ExpandedCommitIndex()); anchor >= 0 {
			m.sidebar.FocusAnchor(anchor, false)
		} else if !m.sidebar.FocusVisible() {
			return
		}
		if commit, ok = m.focusedCommit(); !ok {
			return
		}
	}
	if !m.prView.SetFocusedCommit(commit) {
		return
	}
	anchor := m.sidebar.FocusedAnchor()
	m.renderSidebarPRContent()
	m.sidebar.FocusAnchor(anchor, fromBelow)
}

// focusedCheck returns the index of the check focused in an open
// notification's PR, as listed, if any.
func (m *Model) focusedCheck() (int, bool) {
	if !m.isNotificationSubjectShown() || m.notificationView.GetSubjectPR() == nil {
		return 0, false
	}
	i := m.sidebar.FocusedAnchor()
	if i < 0 || i >= len(m.sidebarComments) || m.sidebarComments[i].Check == nil {
		return 0, false
	}
	return *m.sidebarComments[i].Check, true
}

func (m *Model) hasFocusedCheck() bool {
	_, ok := m.focusedCheck()
	return ok
}

// syncFocusedCheck keeps a check focused in an open notification's PR and
// shows its details, the way syncFocusedCommit does for commits.
func (m *Model) syncFocusedCheck(fromBelow bool) {
	if !m.isNotificationSubjectShown() || m.notificationView.GetSubjectPR() == nil ||
		!m.prView.IsChecksTab() {
		return
	}
	check, ok := m.focusedCheck()
	if !ok {
		if anchor := m.checkAnchor(m.prView.ExpandedCheckIndex()); anchor >= 0 {
			m.sidebar.FocusAnchor(anchor, false)
		} else if !m.sidebar.FocusVisible() {
			return
		}
		if check, ok = m.focusedCheck(); !ok {
			return
		}
	}
	if !m.prView.SetFocusedCheck(check) {
		return
	}
	anchor := m.sidebar.FocusedAnchor()
	m.renderSidebarPRContent()
	m.sidebar.FocusAnchor(anchor, fromBelow)
}

// checkAnchor returns the index of the sidebar anchor of the check at the
// given index, or -1 when it has none.
func (m *Model) checkAnchor(check int) int {
	for i, c := range m.sidebarComments {
		if c.Check != nil && *c.Check == check {
			return i
		}
	}
	return -1
}

// openFocusedCheck opens the check focused in an open notification's PR in
// the browser, e.g. its job's log.
func (m *Model) openFocusedCheck() tea.Cmd {
	check, ok := m.focusedCheck()
	if !ok {
		return nil
	}
	url := m.prView.CheckUrl(check)
	if url == "" {
		m.ctx.Error = errors.New("this check has no page to open")
		return nil
	}
	return m.openUrlInBrowser(url)
}

// commitAnchor returns the index of the sidebar anchor of the commit at the
// given index, or -1 when it has none.
func (m *Model) commitAnchor(commit int) int {
	for i, c := range m.sidebarComments {
		if c.Commit != nil && *c.Commit == commit {
			return i
		}
	}
	return -1
}

// viewFocusedCommitFiles switches an open notification's PR to its files
// tab, narrowed to the focused commit's files.
func (m *Model) viewFocusedCommitFiles() tea.Cmd {
	commit, ok := m.focusedCommit()
	if !ok {
		return nil
	}
	cmd := m.prView.ViewCommitFiles(commit)
	m.sidebar.ScrollToTop()
	m.syncSidebar()
	return cmd
}

func (m *Model) hasFocusedComment() bool {
	_, ok := m.focusedComment()
	return ok
}

// replyToFocusedComment opens the comment editor with the focused comment
// quoted, like GitHub's "Quote reply".
func (m *Model) replyToFocusedComment() tea.Cmd {
	comment, ok := m.focusedComment()
	if !ok {
		return nil
	}
	quote := common.QuoteReply(comment.Body)
	if m.hasDetachedDraft() {
		// Quote it in the draft being written, like GitHub's quote reply
		if m.notificationView.GetSubjectPR() != nil {
			m.prView.AppendToDraft(quote)
		} else {
			m.issueSidebar.AppendToDraft(quote)
		}
		return m.continueDraft()
	}
	// Stay on the current tab so the comment being replied to stays in view
	if m.notificationView.GetSubjectPR() != nil {
		return m.openSidebarForInput(func(bool) tea.Cmd { return m.prView.StartComment(quote, true) })
	}
	return m.openSidebarForInput(func(bool) tea.Cmd { return m.issueSidebar.StartComment(quote, true) })
}

// refreshNotificationSubject refetches the open notification's PR/Issue,
// keeping the tab, scroll position and any draft
func (m *Model) refreshNotificationSubject() tea.Cmd {
	notifId := m.notificationView.GetSubjectId()
	pr, issue := m.notificationView.GetSubjectPR(), m.notificationView.GetSubjectIssue()
	var url string
	var number int
	switch {
	case pr != nil:
		url, number = pr.Primary.Url, pr.Primary.Number
	case issue != nil:
		url, number = issue.Url, issue.Number
	default:
		return nil
	}

	taskId := fmt.Sprintf("notification_refresh_%s", notifId)
	startCmd := m.ctx.StartTask(context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Refreshing #%d", number),
		FinishedText: fmt.Sprintf("Refreshed #%d", number),
		State:        context.TaskStart,
	})
	return tea.Batch(startCmd, func() tea.Msg {
		refreshed := notificationSubjectRefreshedMsg{NotificationId: notifId}
		var err error
		if pr != nil {
			var fetched data.EnrichedPullRequestData
			fetched, err = data.FetchPullRequest(url)
			refreshed.PR = &fetched
		} else {
			var fetched data.IssueData
			fetched, err = data.FetchIssue(url)
			refreshed.Issue = &fetched
		}
		if err != nil {
			return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
		}
		return constants.TaskFinishedMsg{TaskId: taskId, Msg: refreshed}
	})
}

// updateNotificationSubject applies a finished task's update, e.g. a posted
// comment, to the open notification's PR/Issue, which isn't part of any
// section
func (m *Model) updateNotificationSubject(msg tea.Msg) {
	pr, issue := m.notificationView.GetSubjectPR(), m.notificationView.GetSubjectIssue()
	switch msg := msg.(type) {
	case tasks.UpdatePRMsg:
		if pr != nil && pr.Primary.Number == msg.PrNumber && msg.NewComment != nil {
			pr.Enriched.Comments.Nodes = append(pr.Enriched.Comments.Nodes, *msg.NewComment)
		}
	case tasks.UpdateIssueMsg:
		if issue != nil && issue.Number == msg.IssueNumber && msg.NewComment != nil {
			issue.Comments.Nodes = append(issue.Comments.Nodes, *msg.NewComment)
		}
	case notificationSubjectRefreshedMsg:
		// It's stale if another notification has been opened since
		if m.notificationView.GetSubjectId() != msg.NotificationId {
			return
		}
		if msg.PR != nil && pr != nil {
			prData := msg.PR.ToPullRequestData()
			m.notificationView.SetSubjectPR(&prrow.Data{
				Primary:    &prData,
				Enriched:   *msg.PR,
				IsEnriched: true,
			}, msg.NotificationId)
		} else if msg.Issue != nil && issue != nil {
			m.notificationView.SetSubjectIssue(msg.Issue, msg.NotificationId)
		}
	}
}

func (m *Model) syncSidebar() tea.Cmd {
	if !m.sidebar.IsOpen {
		return nil
	}

	currRowData := m.getCurrRowData()
	width := m.sidebar.GetSidebarContentWidth()
	var cmd tea.Cmd

	if currRowData == nil {
		m.sidebar.SetContent("")
		return nil
	}

	switch row := currRowData.(type) {
	case branch.BranchData:
		cmd = m.branchSidebar.SetRow(&row)
		m.sidebar.SetContent(m.branchSidebar.View())
	case *prrow.Data:
		m.prView.SetSectionId(m.currSectionId)
		m.prView.SetRow(row)
		m.prView.SetWidth(width)
		m.setSidebarPRContent()
	case *data.IssueData:
		m.issueSidebar.SetSectionId(m.currSectionId)
		m.issueSidebar.SetRow(row)
		m.issueSidebar.SetWidth(width)
		m.setSidebarIssueContent()
	case *notificationrow.Data:
		notifId := row.GetId()

		// Check if we already have cached data for this notification (user already viewed it)
		if m.notificationView.GetSubjectId() == notifId {
			// Use cached data
			if m.notificationView.GetSubjectPR() != nil {
				m.prView.SetSectionId(0)
				m.prView.SetRow(m.notificationView.GetSubjectPR())
				m.prView.SetWidth(width)
				m.setSidebarPRContent()
			} else if m.notificationView.GetSubjectIssue() != nil {
				m.issueSidebar.SetSectionId(0)
				m.issueSidebar.SetRow(m.notificationView.GetSubjectIssue())
				m.issueSidebar.SetWidth(width)
				m.setSidebarIssueContent()
			}
			return nil
		}

		// Clear cached subject when navigating to a different notification
		// so key dispatch doesn't route keys to the wrong subject's handler.
		m.stashDraft()
		m.notificationView.ClearSubject()
		m.notificationView.StopLoading()
		m.sidebar.ClearSearch()
		keys.SetNotificationSubject(keys.NotificationSubjectNone)
		// Show prompt to view notification (don't auto-fetch)
		// User must press Enter to view content and mark as read
		m.sidebar.SetContent(m.renderNotificationPrompt(row))
	}

	return cmd
}

func (m *Model) renderNotificationPrompt(row *notificationrow.Data) string {
	var content strings.Builder

	subjectType := row.GetSubjectType()
	leftMargin := "      " // Left margin for content

	// Styles
	normalText := lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText)
	faintText := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)
	// Highlighted key style for main prompt (with background)
	highlightKeyStyle := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.PrimaryText).
		Background(m.ctx.Theme.FaintBorder).
		Padding(0, 1)
	// Simple key style for table (no background)
	keyStyle := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.PrimaryText)
	actionStyle := lipgloss.NewStyle().Foreground(m.ctx.Theme.SuccessText)
	headerStyle := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.PrimaryText).
		Bold(true)

	// Determine subject type display name and primary action
	typeName := "PR"
	enterAction := "view"
	if subjectType == "Issue" {
		typeName = "Issue"
	} else if subjectType != "PullRequest" {
		typeName = subjectType
		enterAction = "open in browser"
	}

	// Main prompt: "Press Enter to view the PR" or "Press Enter to open in browser"
	content.WriteString("\n")
	content.WriteString(leftMargin)
	content.WriteString(normalText.Render("Press "))
	content.WriteString(highlightKeyStyle.Render("Enter"))
	if enterAction == "view" {
		content.WriteString(normalText.Render(fmt.Sprintf(" to %s the %s", enterAction, typeName)))
	} else {
		content.WriteString(normalText.Render(fmt.Sprintf(" to %s", enterAction)))
	}
	content.WriteString("\n")

	// Note about marking as read
	content.WriteString(leftMargin)
	content.WriteString(faintText.Render("(Note: this will mark it as read)"))
	content.WriteString("\n")

	content.WriteString("\n")

	// Other Actions header
	content.WriteString(leftMargin)
	content.WriteString(headerStyle.Render("Other Actions"))
	content.WriteString("\n\n")

	// Key-action pairs (simple list without borders)
	actions := []struct {
		key    string
		action string
	}{
		{"D", "mark as done"},
		{"m", "mark as read"},
		{"u", "unsubscribe"},
		{"b", "toggle bookmark"},
		{"t", "toggle filtering"},
		{"S", "sort by repo"},
		{"o", "open in browser"},
	}

	keyWidth := 7 // Width for key column
	for _, a := range actions {
		content.WriteString(leftMargin)
		// Right-align the key in its column
		padding := strings.Repeat(" ", keyWidth-len(a.key))
		content.WriteString(padding)
		content.WriteString(keyStyle.Render(a.key))
		content.WriteString("  ")
		content.WriteString(actionStyle.Render(a.action))
		content.WriteString("\n")
	}

	// Add Enter and Esc at the end
	content.WriteString(leftMargin)
	padding := strings.Repeat(" ", keyWidth-len("Enter"))
	content.WriteString(padding)
	content.WriteString(keyStyle.Render("Enter"))
	content.WriteString("  ")
	content.WriteString(actionStyle.Render(enterAction))
	content.WriteString("\n")
	content.WriteString(leftMargin)
	escPadding := strings.Repeat(" ", keyWidth-len("Esc"))
	content.WriteString(escPadding)
	content.WriteString(keyStyle.Render("Esc"))
	content.WriteString("  ")
	content.WriteString(actionStyle.Render("go back"))

	return content.String()
}

// loadNotificationContent fetches and displays notification content, marking it as read
func (m *Model) loadNotificationContent() tea.Cmd {
	currRowData := m.getCurrRowData()
	row, ok := currRowData.(*notificationrow.Data)
	if !ok || row == nil {
		return nil
	}

	notifId := row.GetId()
	subjectType := row.GetSubjectType()
	subjectUrl := row.GetUrl()
	latestCommentUrl := row.GetLatestCommentUrl()

	// Show loading indicator
	var spinnerCmd tea.Cmd
	if subjectType == "PullRequest" || subjectType == "Issue" {
		spinnerCmd = m.notificationView.StartLoading(notifId)
	}
	width := m.sidebar.GetSidebarContentWidth()
	m.notificationView.SetRow(row)
	m.notificationView.SetWidth(width)
	m.sidebar.SetContent(m.notificationView.View())

	switch subjectType {
	case "PullRequest":
		return tea.Batch(
			spinnerCmd,
			func() tea.Msg {
				_ = data.MarkNotificationRead(notifId)
				return notificationssection.UpdateNotificationReadStateMsg{
					Id:     notifId,
					Unread: false,
				}
			},
			func() tea.Msg {
				pr, err := data.FetchPullRequest(subjectUrl)
				return notificationPRFetchedMsg{
					NotificationId:   notifId,
					PR:               pr,
					LatestCommentUrl: latestCommentUrl,
					Err:              err,
				}
			},
		)
	case "Issue":
		return tea.Batch(
			spinnerCmd,
			func() tea.Msg {
				_ = data.MarkNotificationRead(notifId)
				return notificationssection.UpdateNotificationReadStateMsg{
					Id:     notifId,
					Unread: false,
				}
			},
			func() tea.Msg {
				issue, err := data.FetchIssue(subjectUrl)
				return notificationIssueFetchedMsg{
					NotificationId:   notifId,
					Issue:            issue,
					LatestCommentUrl: latestCommentUrl,
					Err:              err,
				}
			},
		)
	default:
		// For discussions, releases, etc. - mark as read and open in browser
		// since we can't show rich content for these types
		return tea.Batch(
			func() tea.Msg {
				_ = data.MarkNotificationRead(notifId)
				return notificationssection.UpdateNotificationReadStateMsg{
					Id:     notifId,
					Unread: false,
				}
			},
			m.openBrowser(),
		)
	}
}

func (m *Model) fetchAllViewSections() tea.Cmd {
	cmds := make([]tea.Cmd, 0)
	cmds = append(cmds, m.tabs.SetAllLoading()...)

	switch m.ctx.View {
	case config.RepoView:
		var cmd tea.Cmd
		s, cmd := reposection.FetchAllBranches(m.ctx)
		cmds = append(cmds, cmd)
		m.repo = &s
		return tea.Batch(cmds...)
	case config.NotificationsView:
		notifCmd := notificationssection.FetchAllSections(m.ctx, m.notifications)
		cmds = append(cmds, notifCmd)
		return tea.Batch(cmds...)
	case config.PRsView:
		prcmds := prssection.FetchAllSections(m.ctx, m.prs)
		cmds = append(cmds, prcmds)
		return tea.Batch(cmds...)
	default:
		issuecmds := issuessection.FetchAllSections(m.ctx, m.issues)
		cmds = append(cmds, issuecmds)
		return tea.Batch(cmds...)
	}
}

func (m *Model) getCurrentViewSections() []section.Section {
	switch m.ctx.View {
	case config.RepoView:
		if m.repo == nil {
			return []section.Section{}
		}
		return []section.Section{m.repo}
	case config.NotificationsView:
		if len(m.notifications) == 0 {
			return []section.Section{}
		}
		return m.notifications
	case config.PRsView:
		return m.prs
	default:
		return m.issues
	}
}

// rebuildAllSectionRows re-renders every section's rows, e.g. when colors
// change, since rows are rendered to styled strings when their data arrives.
func (m *Model) rebuildAllSectionRows() {
	sections := slices.Concat(m.prs, m.issues, m.notifications)
	if m.repo != nil {
		sections = append(sections, m.repo)
	}
	for _, s := range sections {
		if s != nil {
			s.SetRows(s.BuildRows())
		}
	}
}

func (m *Model) updateTabs() {
	sections := m.getCurrentViewSections()
	m.tabs.SetSections(sections)
}

func (m *Model) switchSelectedView() tea.Cmd {
	repoFF := config.IsFeatureEnabled(config.FF_REPO_VIEW)

	// Reset notification subject when leaving notifications view
	if m.ctx.View == config.NotificationsView {
		keys.SetNotificationSubject(keys.NotificationSubjectNone)
		m.stashDraft()
		m.notificationView.ClearSubject()
	}

	// View cycle: Notifications → PRs → Issues (→ Repo if enabled) → Notifications
	if repoFF {
		switch m.ctx.View {
		case config.NotificationsView:
			m.ctx.View = config.PRsView
		case config.PRsView:
			m.ctx.View = config.IssuesView
		case config.IssuesView:
			m.ctx.View = config.RepoView
		case config.RepoView:
			m.ctx.View = config.NotificationsView
		}
	} else {
		switch m.ctx.View {
		case config.NotificationsView:
			m.ctx.View = config.PRsView
		case config.PRsView:
			m.ctx.View = config.IssuesView
		default:
			m.ctx.View = config.NotificationsView
		}
	}

	m.syncMainContentDimensions()
	m.setCurrSectionId(0)

	var cmds []tea.Cmd
	if m.isViewSectionsStale() {
		cmds = append(cmds, m.fetchAllViewSections())
	}
	m.updateTabs()
	cmds = append(cmds, m.onViewedRowChanged())

	return tea.Batch(cmds...)
}

func (m *Model) isUserDefinedKeybinding(msg tea.KeyMsg) bool {
	if m.ctx == nil || m.ctx.Config == nil {
		return false
	}
	for _, keybinding := range m.ctx.Config.Keybindings.Universal {
		if keybinding.Builtin == "" && keybinding.Key == msg.String() {
			return true
		}
	}

	if m.ctx.View == config.IssuesView {
		for _, keybinding := range m.ctx.Config.Keybindings.Issues {
			if keybinding.Builtin == "" && keybinding.Key == msg.String() {
				return true
			}
		}
	}

	if m.ctx.View == config.PRsView {
		for _, keybinding := range m.ctx.Config.Keybindings.Prs {
			if keybinding.Builtin == "" && keybinding.Key == msg.String() {
				return true
			}
		}
	}

	if m.ctx.View == config.RepoView {
		for _, keybinding := range m.ctx.Config.Keybindings.Branches {
			if keybinding.Builtin == "" && keybinding.Key == msg.String() {
				return true
			}
		}
	}

	if m.ctx.View == config.NotificationsView {
		for _, keybinding := range m.ctx.Config.Keybindings.Notifications {
			if keybinding.Builtin == "" && keybinding.Key == msg.String() {
				return true
			}
		}

		currRowData := m.getCurrRowData()
		if nData, ok := currRowData.(*notificationrow.Data); ok {
			switch nData.Notification.Subject.Type {
			case "PullRequest":
				for _, keybinding := range m.ctx.Config.Keybindings.Prs {
					if keybinding.Builtin == "" && keybinding.Key == msg.String() {
						return true
					}
				}
			case "Issue":
				for _, keybinding := range m.ctx.Config.Keybindings.Issues {
					if keybinding.Builtin == "" && keybinding.Key == msg.String() {
						return true
					}
				}
			}
		}
	}

	return false
}

func (m *Model) renderRunningTask() string {
	tasks := make([]context.Task, 0, len(m.tasks))
	for _, value := range m.tasks {
		tasks = append(tasks, value)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].FinishedTime != nil && tasks[j].FinishedTime == nil {
			return false
		}
		if tasks[j].FinishedTime != nil && tasks[i].FinishedTime == nil {
			return true
		}
		if tasks[j].FinishedTime != nil && tasks[i].FinishedTime != nil {
			return tasks[i].FinishedTime.After(*tasks[j].FinishedTime)
		}

		return tasks[i].StartTime.After(tasks[j].StartTime)
	})
	task := tasks[0]

	var currTaskStatus string
	switch task.State {
	case context.TaskStart:
		currTaskStatus = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.taskSpinner.View(),
			lipgloss.NewStyle().
				Background(m.ctx.Theme.SelectedBackground).Render(task.StartText),
		)
	case context.TaskError:
		currTaskStatus = lipgloss.NewStyle().
			Foreground(m.ctx.Theme.ErrorText).
			Background(m.ctx.Theme.SelectedBackground).
			Render(fmt.Sprintf("%s %s", constants.FailureIcon, task.Error.Error()))
	case context.TaskFinished:
		currTaskStatus = lipgloss.NewStyle().
			Foreground(m.ctx.Theme.SuccessText).
			Background(m.ctx.Theme.SelectedBackground).
			Render(fmt.Sprintf("%s %s", constants.SuccessIcon, task.FinishedText))
	}

	var numProcessing int
	for _, task := range m.tasks {
		if task.State == context.TaskStart {
			numProcessing += 1
		}
	}

	stats := ""
	if numProcessing > 1 {
		stats = lipgloss.NewStyle().
			Foreground(m.ctx.Theme.FaintText).
			Background(m.ctx.Theme.SelectedBackground).
			Render(fmt.Sprintf("[ %d] ", numProcessing))
	}

	return lipgloss.NewStyle().
		Padding(0, 1).
		Height(1).
		Background(m.ctx.Theme.SelectedBackground).
		Render(strings.TrimSpace(lipgloss.JoinHorizontal(lipgloss.Top, stats, currTaskStatus)))
}

type userFetchedMsg struct {
	user string
}

func fetchUser() tea.Msg {
	user, err := data.CurrentLoginName()
	if err != nil {
		return constants.ErrMsg{
			Err: err,
		}
	}

	return userFetchedMsg{
		user: user,
	}
}

type intervalRefresh time.Time

func (m *Model) doRefreshAtInterval() tea.Cmd {
	if m.ctx.Config.Defaults.RefetchIntervalMinutes == 0 {
		return nil
	}

	return tea.Tick(
		time.Minute*time.Duration(m.ctx.Config.Defaults.RefetchIntervalMinutes),
		func(t time.Time) tea.Msg {
			return intervalRefresh(t)
		},
	)
}

type updateFooterMsg struct{}

type pollBackgroundColorMsg struct{}

// backgroundPollInterval is how often we re-query the background color in
// terminals that can't notify us when their color scheme changes.
const backgroundPollInterval = time.Second

func pollBackgroundColor() tea.Cmd {
	return tea.Tick(backgroundPollInterval, func(time.Time) tea.Msg {
		return pollBackgroundColorMsg{}
	})
}

func (m *Model) doUpdateFooterAtInterval() tea.Cmd {
	return tea.Tick(
		time.Second*10,
		func(t time.Time) tea.Msg {
			return updateFooterMsg{}
		},
	)
}

// promptConfirmationForNotificationPR shows a confirmation prompt for PR actions
// when viewing a PR from a notification. This is separate from section-based
// confirmation because the notification section doesn't know about PR actions.
func (m *Model) promptConfirmationForNotificationPR(action string) tea.Cmd {
	prompt := m.notificationView.SetPendingPRAction(action)
	if prompt == "" {
		return nil
	}
	m.footer.SetLeftSection(m.ctx.Styles.ListViewPort.PagerStyle.Render(prompt))
	return nil
}

// promptConfirmationForNotificationIssue shows a confirmation prompt for Issue actions
// when viewing an Issue from a notification.
func (m *Model) promptConfirmationForNotificationIssue(action string) tea.Cmd {
	prompt := m.notificationView.SetPendingIssueAction(action)
	if prompt == "" {
		return nil
	}
	m.footer.SetLeftSection(m.ctx.Styles.ListViewPort.PagerStyle.Render(prompt))
	return nil
}

// executeNotificationAction executes a PR/Issue action after user confirmation
func (m *Model) executeNotificationAction(action string) tea.Cmd {
	if action == "" {
		return nil
	}

	sid := tasks.SectionIdentifier{Id: m.currSectionId, Type: notificationssection.SectionType}
	pr := m.notificationView.GetSubjectPR()
	issue := m.notificationView.GetSubjectIssue()

	switch action {
	case "pr_close":
		if pr != nil {
			return tasks.ClosePR(m.ctx, sid, pr)
		}
	case "pr_reopen":
		if pr != nil {
			return tasks.ReopenPR(m.ctx, sid, pr)
		}
	case "pr_ready":
		if pr != nil {
			return tasks.PRReady(m.ctx, sid, pr)
		}
	case "pr_merge":
		if pr != nil {
			return tasks.MergePR(m.ctx, sid, pr)
		}
	case "pr_update":
		if pr != nil {
			return tasks.UpdatePR(m.ctx, sid, pr)
		}
	case "pr_approveWorkflows":
		if pr != nil {
			return tasks.ApproveWorkflows(m.ctx, sid, pr)
		}
	case "issue_close":
		if issue != nil {
			return tasks.CloseIssue(m.ctx, sid, issue)
		}
	case "issue_reopen":
		if issue != nil {
			return tasks.ReopenIssue(m.ctx, sid, issue)
		}
	}

	return nil
}

func (m *Model) addNewSection() {
	var sid int
	switch m.ctx.View {
	case config.IssuesView:
		sid = len(m.issues)
		m.issues = append(m.issues, new(issuessection.NewModel(
			sid,
			m.ctx,
			config.IssuesSectionConfig{
				Title:   "Scratch",
				Filters: "archived:false",
			},
			time.Now(),
			time.Now(),
		)))
	case config.NotificationsView:
		sid = len(m.notifications)
		m.notifications = append(m.notifications, new(notificationssection.NewModel(
			sid,
			m.ctx,
			config.NotificationsSectionConfig{
				Title:   "Scratch",
				Filters: "archived:false",
			},
			time.Now(),
		)))
	case config.PRsView:
		sid = len(m.prs)
		m.prs = append(m.prs, new(prssection.NewModel(
			sid,
			m.ctx,
			config.PrsSectionConfig{
				Title:   "Scratch",
				Filters: "archived:false",
			},
			time.Now(),
			time.Now(),
		)))
	}
	m.updateTabs()
	m.setCurrSectionId(sid)
}

func (m *Model) removeSection() {
	cnt := len(m.getCurrentViewSections())
	// don't allow removing the last section
	if cnt <= 1 {
		return
	}

	del := m.currSectionId
	switch m.ctx.View {
	case config.IssuesView:
		m.issues = slices.Delete(m.issues, del, del+1)
	case config.NotificationsView:
		m.notifications = slices.Delete(m.notifications, del, del+1)
	case config.PRsView:
		m.prs = slices.Delete(m.prs, del, del+1)
	}

	for i, s := range m.getCurrentViewSections() {
		s.SetId(i)
	}

	nCnt := len(m.getCurrentViewSections())
	m.updateTabs()
	m.setCurrSectionId(min(nCnt-1, del))
}

func (m *Model) initSections() {
	m.prs = prssection.InitSections(m.ctx)
	m.issues = issuessection.InitSections(m.ctx)
	m.notifications = notificationssection.InitSections(m.ctx)
}

func (m *Model) isViewSectionsStale() bool {
	for _, section := range m.getCurrentViewSections() {
		if section.IsDataStale() {
			return true
		}
	}
	return false
}
