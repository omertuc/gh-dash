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

// typedSource treats the whole input as what the user is completing.
type typedSource struct{ twoSuggestions }

func (typedSource) ExtractContext(input string, _ tea.Position) fuzzyselect.Context {
	return fuzzyselect.Context{Content: input}
}

func TestEnterAcceptsSuggestionOnlyWhenPicked(t *testing.T) {
	th := *theme.DefaultTheme
	ctx := &context.ProgramContext{Config: &config.Config{}, Theme: th, Styles: context.InitStyles(th)}
	ti := DefaultTextInput(ctx)
	m := NewModel(ctx, ModelOpts{TextInput: &ti})
	fzf := fuzzyselect.NewModel(ctx, typedSource{})
	m.SetAutocomplete(&fzf)
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}

	show := func(value string) {
		m.SetValue(value)
		fzf.Filter(value, m.CurrentAutocompleteContext(), nil)
		fzf.Show()
	}

	show("al")
	fzf.Hide()
	if m.AcceptsSuggestion(enter) {
		t.Fatal("enter with the popup hidden should not accept a suggestion")
	}

	show("")
	if m.AcceptsSuggestion(enter) {
		t.Fatal("enter with nothing typed or picked should not accept a suggestion")
	}
	fzf.Next()
	if !m.AcceptsSuggestion(enter) {
		t.Fatal("enter after moving through the list should accept the suggestion")
	}

	show("al")
	if !m.AcceptsSuggestion(enter) {
		t.Fatal("enter partway through a suggestion should accept it")
	}

	show("alice")
	if m.AcceptsSuggestion(enter) {
		t.Fatal("enter with the suggestion already typed should not accept it")
	}
}
