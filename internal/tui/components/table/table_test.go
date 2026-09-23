package table

import (
	"fmt"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestTable(numRows int) Model {
	cfg := config.Config{Theme: &config.ThemeConfig{}}
	cfg.Theme.Ui.Table.ShowSeparator = true
	th := *theme.DefaultTheme
	ctx := context.ProgramContext{Config: &cfg, Theme: th, Styles: context.InitStyles(th)}
	iconWidth, activityWidth, updatedWidth := 3, 6, 10
	grow := true
	columns := []Column{
		{Title: "T", Width: &iconWidth},
		{Title: "Title", Grow: &grow},
		{Title: "A", Width: &activityWidth},
		{Title: "U", Width: &updatedWidth},
	}
	m := NewModel(ctx, constants.Dimensions{Width: 120, Height: 40},
		time.Now(), time.Now(), columns, nil, "items", nil, "", false)
	m.SetContentHeight(3)
	m.SetRows(testRows(numRows, "title"))
	return m
}

func testRows(n int, title string) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = testRow(i, title)
	}
	return rows
}

func testRow(i int, title string) Row {
	return Row{
		"\x1b[32m\n\x1b[34m●\n",
		fmt.Sprintf("owner/repo #%d\n\x1b[1m%s %d\n@someone commented", i, title, i),
		"\n\n",
		"3h ago\n\n",
	}
}

// fullRender returns what the table would show if every row were rendered
// from scratch, the way it was before the render cache existed.
func fullRender(m Model) string {
	headerColumns := m.renderHeaderColumns()
	rendered := make([]string, 0, len(m.Rows))
	for i := range m.Rows {
		rendered = append(rendered, m.renderRow(i, headerColumns))
	}
	m.rowsViewport.SyncViewPort(lipgloss.JoinVertical(lipgloss.Left, rendered...))
	return m.View()
}

func TestIncrementalRenderMatchesFullRender(t *testing.T) {
	m := newTestTable(30)

	for range 12 {
		m.NextItem()
		if got, want := m.View(), fullRender(m); got != want {
			t.Fatalf("after NextItem to row %d: incremental render differs from full render", m.GetCurrItem())
		}
	}
	for range 5 {
		m.PrevItem()
		if got, want := m.View(), fullRender(m); got != want {
			t.Fatalf("after PrevItem to row %d: incremental render differs from full render", m.GetCurrItem())
		}
	}

	m.SetRow(3, testRow(3, "changed"))
	if got, want := m.View(), fullRender(m); got != want {
		t.Fatal("after SetRow: incremental render differs from full render")
	}

	m.SetRows(testRows(30, "other"))
	if got, want := m.View(), fullRender(m); got != want {
		t.Fatal("after SetRows: incremental render differs from full render")
	}

	m.SetRows(testRows(10, "fewer"))
	if got, want := m.View(), fullRender(m); got != want {
		t.Fatal("after shrinking rows: incremental render differs from full render")
	}

	m.SetDimensions(constants.Dimensions{Width: 90, Height: 40})
	m.SyncViewPortContent()
	if got, want := m.View(), fullRender(m); got != want {
		t.Fatal("after resize: incremental render differs from full render")
	}
}

func TestInPlaceRowEditIsDetected(t *testing.T) {
	m := newTestTable(5)
	m.Rows[2][1] = "edited in place"
	m.SyncViewPortContent()
	if got, want := m.View(), fullRender(m); got != want {
		t.Fatal("in-place row edit was not re-rendered")
	}
}

func BenchmarkNextItem(b *testing.B) {
	for _, n := range []int{20, 100, 500} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			m := newTestTable(n)
			for b.Loop() {
				if m.NextItem() == n-1 {
					m.FirstItem()
				}
				_ = m.View()
			}
		})
	}
}

// BenchmarkSyncUnchanged measures the per-message cost of syncing a table
// whose rows haven't changed, which happens on every program context update.
func BenchmarkSyncUnchanged(b *testing.B) {
	m := newTestTable(100)
	for b.Loop() {
		m.SyncViewPortContent()
	}
}

// BenchmarkViewUnchanged measures rendering a frame when the table itself
// hasn't changed, e.g. when an unrelated message triggers a redraw.
func BenchmarkViewUnchanged(b *testing.B) {
	m := newTestTable(100)
	for b.Loop() {
		m.SyncViewPortContent()
		_ = m.View()
	}
}
