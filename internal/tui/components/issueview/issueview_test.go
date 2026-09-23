package issueview

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestContext(t *testing.T) *context.ProgramContext {
	t.Helper()

	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	thm := theme.ParseTheme(&cfg)
	return &context.ProgramContext{
		Config:            &cfg,
		Theme:             thm,
		Styles:            context.InitStyles(thm),
		HasDarkBackground: true,
		BackgroundSource:  "default",
	}
}

func TestNewModelSetsProgramContext(t *testing.T) {
	ctx := newTestContext(t)
	m := NewModel(ctx)

	require.NotNil(t, m.ctx)
	require.Same(t, ctx, m.ctx)
}

func TestRenderBodyDoesNotPanicBeforeContextSync(t *testing.T) {
	ctx := newTestContext(t)
	m := NewModel(ctx)
	m.SetWidth(80)
	m.SetRow(&data.IssueData{
		Title: "Example issue",
		Body:  "Hello **world**",
	})

	require.NotPanics(t, func() {
		_ = m.renderBody()
	})
}

func TestIssueBodyAnchorsPointAtCommentStarts(t *testing.T) {
	ctx := newTestContext(t)
	m := NewModel(ctx)
	m.SetWidth(80)
	issue := &data.IssueData{
		Title: "Example issue",
		Body:  "A description\n\nwith a few\n\n- lines\n- of **markdown**",
	}
	for i := range 3 {
		issue.Comments.Nodes = append(issue.Comments.Nodes, data.IssueComment{
			Body:      fmt.Sprintf("Comment number %d\n\nwith a second paragraph", i),
			UpdatedAt: time.Now().Add(time.Duration(i) * time.Minute),
		})
	}
	m.SetRow(issue)

	body, anchors := m.ViewBodyWithAnchors()
	require.Len(t, anchors, 3)
	lines := strings.Split(ansi.Strip(body), "\n")
	for i, anchor := range anchors {
		// Each comment starts with the top border of its author box
		require.True(t, strings.HasPrefix(strings.TrimSpace(lines[anchor.Line]), "╭"),
			"anchor %d at line %d doesn't start a comment: %q", i, anchor.Line, lines[anchor.Line])
	}
}
