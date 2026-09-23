package inputbox

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/fuzzyselect"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

// twoSuggestions always suggests the same two users.
type twoSuggestions struct{}

func (twoSuggestions) ExtractContext(string, tea.Position) fuzzyselect.Context {
	return fuzzyselect.Context{}
}

func (twoSuggestions) InsertSuggestion(input, _ string, _, _ tea.Position) (string, tea.Position) {
	return input, tea.Position{}
}

func (twoSuggestions) ItemsToExclude(string, tea.Position) []string { return nil }

func (twoSuggestions) Suggestions(string, tea.Position) []fuzzyselect.Suggestion {
	return []fuzzyselect.Suggestion{{Value: "alice"}, {Value: "bob"}}
}

func (twoSuggestions) LoadSuggestions(fuzzyselect.LoaderContext) error { return nil }

func TestArrowKeysMoveCursorWhileSuggestionsHidden(t *testing.T) {
	th := *theme.DefaultTheme
	ctx := &context.ProgramContext{Config: &config.Config{}, Theme: th, Styles: context.InitStyles(th)}
	ta := DefaultTextArea(ctx)
	m := NewModel(ctx, ModelOpts{TextArea: &ta})
	fzf := fuzzyselect.NewModel(ctx, twoSuggestions{})
	m.SetAutocomplete(&fzf)
	m.SetWidth(60)
	m.SetValue("line one\nline two")

	// Suggestions are loaded but the popup is hidden
	fzf.Filter(m.Value(), fuzzyselect.Context{}, nil)
	fzf.Hide()
	if !fzf.HasSuggestions() || fzf.IsVisible() {
		t.Fatal("test setup: expected hidden suggestions")
	}

	if got := m.textArea.Line(); got != 1 {
		t.Fatalf("test setup: cursor on line %d, want 1", got)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.textArea.Line(); got != 0 {
		t.Fatalf("up with suggestions hidden: cursor on line %d, want 0", got)
	}

	// With the popup showing, the arrows move through the suggestions
	fzf.Show()
	selected := fzf.Selected()
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if fzf.Selected() == selected {
		t.Fatal("down with suggestions showing should select the next suggestion")
	}
	if got := m.textArea.Line(); got != 0 {
		t.Fatalf("down with suggestions showing moved the cursor to line %d", got)
	}
}
