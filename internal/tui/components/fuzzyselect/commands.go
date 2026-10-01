package fuzzyselect

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// CommandSource suggests commands, e.g. Prow's "/lgtm", on lines starting
// with "/", and otherwise defers to another source, e.g. mentions.
type CommandSource struct {
	Commands []Suggestion
	// Fallback suggests everywhere else
	Fallback Source
}

// commandStart returns where the command on the cursor's line starts, if the
// line starts with "/" and the cursor is past it.
func commandStart(input string, cursorPos tea.Position) (int, []rune, bool) {
	lines := lines(input)
	if cursorPos.Y < 0 || cursorPos.Y >= len(lines) {
		return 0, nil, false
	}
	runes := []rune(lines[cursorPos.Y])
	start := 0
	for start < len(runes) && isWhitespace(runes[start]) {
		start++
	}
	if start >= len(runes) || runes[start] != '/' || cursorPos.X <= start {
		return 0, nil, false
	}
	return start, runes, true
}

// InCommand reports whether the cursor is on a command, where commands are
// suggested.
func (src *CommandSource) InCommand(input string, cursorPos tea.Position) bool {
	_, _, ok := commandStart(input, cursorPos)
	return ok && len(src.Commands) > 0
}

// ExtractContext returns the whole command, e.g. "/test e2e", as a command
// may have arguments.
func (src *CommandSource) ExtractContext(input string, cursorPos tea.Position) Context {
	if start, runes, ok := commandStart(input, cursorPos); ok && len(src.Commands) > 0 {
		end := len(runes)
		for end > start && isWhitespace(runes[end-1]) {
			end--
		}
		return Context{
			Start:   tea.Position{X: start, Y: cursorPos.Y},
			End:     tea.Position{X: end, Y: cursorPos.Y},
			Content: string(runes[start:end]),
		}
	}
	if src.Fallback == nil {
		return Context{}
	}
	return src.Fallback.ExtractContext(input, cursorPos)
}

// isCommandContext reports whether a context, given by where it starts,
// is a command's.
func (src *CommandSource) isCommandContext(input string, contextStart tea.Position) bool {
	lines := lines(input)
	if len(src.Commands) == 0 || contextStart.Y < 0 || contextStart.Y >= len(lines) {
		return false
	}
	runes := []rune(lines[contextStart.Y])
	if contextStart.X < 0 || contextStart.X >= len(runes) || runes[contextStart.X] != '/' {
		return false
	}
	return strings.TrimSpace(string(runes[:contextStart.X])) == ""
}

func (src *CommandSource) Suggestions(input string, contextStart tea.Position) []Suggestion {
	if src.isCommandContext(input, contextStart) {
		return src.Commands
	}
	if src.Fallback == nil {
		return nil
	}
	return src.Fallback.Suggestions(input, contextStart)
}

func (src *CommandSource) InsertSuggestion(
	input string,
	suggestion string,
	contextStart tea.Position,
	contextEnd tea.Position,
) (string, tea.Position) {
	if !src.isCommandContext(input, contextStart) {
		return src.Fallback.InsertSuggestion(input, suggestion, contextStart, contextEnd)
	}
	lines := lines(input)
	runes := []rune(lines[contextStart.Y])
	lines[contextStart.Y] = string(runes[:contextStart.X]) + suggestion + string(runes[contextEnd.X:])
	return joinLines(lines), tea.Position{
		X: contextStart.X + len([]rune(suggestion)),
		Y: contextStart.Y,
	}
}

func (src *CommandSource) ItemsToExclude(input string, cursorPos tea.Position) []string {
	if src.InCommand(input, cursorPos) || src.Fallback == nil {
		return nil
	}
	return src.Fallback.ItemsToExclude(input, cursorPos)
}

func (src *CommandSource) LoadSuggestions(ctx LoaderContext) error {
	if src.Fallback == nil {
		return nil
	}
	return src.Fallback.LoadSuggestions(ctx)
}
