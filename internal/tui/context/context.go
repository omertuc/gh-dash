package context

import (
	"time"

	tea "charm.land/bubbletea/v2"

	gitm "github.com/aymanbagabas/git-module"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type State = int

const (
	TaskStart State = iota
	TaskFinished
	TaskError
)

type Task struct {
	Id           string
	StartText    string
	FinishedText string
	State        State
	Error        error
	StartTime    time.Time
	FinishedTime *time.Time
}

type ProgramContext struct {
	GHRepo               *repository.Repository
	GitRepo              *gitm.Repository
	RepoPath             string
	RepoUrl              string
	User                 string
	ScreenHeight         int
	ScreenWidth          int
	MainContentWidth     int
	MainContentHeight    int
	DynamicPreviewWidth  int
	DynamicPreviewHeight int    // calculated preview height for bottom mode
	PreviewPosition      string // resolved "right" or "bottom"
	SidebarOpen          bool
	// PreviewFullscreen is set when the preview takes the whole content
	// area and the section isn't shown, e.g. a notification's PR or Issue
	PreviewFullscreen bool
	HasDarkBackground bool
	BackgroundSource  string
	Config            *config.Config
	ConfigFlag        string
	Version           string
	View              config.ViewType
	Error             error
	StartTask         func(task Task) tea.Cmd
	Theme             theme.Theme
	Styles            Styles
	// PendingComments are comments shown before they're posted, which are
	// grayed out until posting them finishes, mapped to the URL of the
	// PR/Issue they're on
	PendingComments map[PendingComment]string
}

// PendingComment identifies a comment that's still being posted
type PendingComment struct {
	Body string
	At   int64 // UnixNano of the comment's UpdatedAt
}

func NewPendingComment(body string, at time.Time) PendingComment {
	return PendingComment{Body: body, At: at.UnixNano()}
}

// AddPendingComment adds c, which is being posted on the PR/Issue at url
func (ctx *ProgramContext) AddPendingComment(c PendingComment, url string) {
	if ctx.PendingComments == nil {
		ctx.PendingComments = map[PendingComment]string{}
	}
	ctx.PendingComments[c] = url
}

func (ctx *ProgramContext) IsPendingComment(body string, at time.Time) bool {
	_, ok := ctx.PendingComments[NewPendingComment(body, at)]
	return ok
}

// HasPendingCommentOn reports whether a comment is being posted on the
// PR/Issue at url
func (ctx *ProgramContext) HasPendingCommentOn(url string) bool {
	for _, u := range ctx.PendingComments {
		if u == url {
			return true
		}
	}
	return false
}

// RepoLocalPath returns the local clone of repoName, from the repoPaths
// config or, failing that, the repo gh-dash was started from.
func (ctx *ProgramContext) RepoLocalPath(repoName string) (string, error) {
	var currentRepoName string
	if ctx.HasGHRepo() {
		currentRepoName = ctx.GHRepo.Owner + "/" + ctx.GHRepo.Name
	}
	var cfgPaths map[string]string
	if ctx.Config != nil {
		cfgPaths = ctx.Config.RepoPaths
	}
	return common.ResolveRepoLocalPath(repoName, cfgPaths, currentRepoName, ctx.RepoPath)
}

func (ctx *ProgramContext) HasGHRepo() bool {
	return ctx.GHRepo != nil && *ctx.GHRepo != (repository.Repository{})
}

func (ctx *ProgramContext) GetViewSectionsConfig() []config.SectionConfig {
	var configs []config.SectionConfig
	switch ctx.View {
	case config.RepoView:
		t := config.RepoView
		configs = append(configs, config.PrsSectionConfig{
			Title:   "Local Branches",
			Filters: "author:@me is:open",
			Limit:   utils.IntPtr(20),
			Type:    &t,
		}.ToSectionConfig())
	case config.NotificationsView:
		for _, cfg := range ctx.Config.NotificationsSections {
			configs = append(configs, cfg.ToSectionConfig())
		}
	case config.PRsView:
		for _, cfg := range ctx.Config.PRSections {
			configs = append(configs, cfg.ToSectionConfig())
		}
	case config.IssuesView:
		for _, cfg := range ctx.Config.IssuesSections {
			configs = append(configs, cfg.ToSectionConfig())
		}
	}

	return append([]config.SectionConfig{{Title: ""}}, configs...)
}

func (ctx *ProgramContext) PreviewCursorPosition() tea.Position {
	if ctx.PreviewFullscreen {
		return tea.Position{X: 0, Y: ctx.Styles.Pager.Height}
	}
	if ctx.PreviewPosition == "right" {
		return tea.Position{
			X: ctx.MainContentWidth,
			Y: ctx.Styles.Pager.Height,
		}
	}

	return tea.Position{
		X: 0,
		Y: ctx.MainContentHeight,
	}
}
