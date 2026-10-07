package keys

import (
	"os/exec"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// physicalKeyFlags ask a terminal speaking the Kitty keyboard protocol to
// report every key as an escape code that includes where the key is on a US
// layout, along with the text it types, so shortcuts can go by the key
// pressed rather than by what it types in the current layout. Bubble Tea
// itself only asks for disambiguated escape codes.
const physicalKeyFlags = ansi.KittyDisambiguateEscapeCodes |
	ansi.KittyReportAlternateKeys |
	ansi.KittyReportAllKeysAsEscapeCodes |
	ansi.KittyReportAssociatedKeys

// RequestPhysicalKeys asks the terminal to report where keys are on a US
// layout. It's meant to be sent once the terminal said it speaks the Kitty
// keyboard protocol, i.e. on a [tea.KeyboardEnhancementsMsg], as Bubble Tea
// sets its own flags before then. Bubble Tea resets them when it exits.
func RequestPhysicalKeys() tea.Cmd {
	return tea.Raw(ansi.KittyKeyboard(physicalKeyFlags, 1))
}

// ExecProcess runs c like [tea.ExecProcess], asking for physical keys again
// once it's done, as Bubble Tea sets its own keyboard flags anew when it
// takes the terminal back.
func ExecProcess(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return tea.ExecProcess(c, func(err error) tea.Msg {
		var msg tea.Msg
		if fn != nil {
			msg = fn(err)
		}
		return tea.BatchMsg{func() tea.Msg { return msg }, RequestPhysicalKeys()}
	})
}

// usShifted is what shift types with the keys of a US layout that aren't
// letters.
var usShifted = map[rune]rune{
	'`': '~', '1': '!', '2': '@', '3': '#', '4': '$', '5': '%', '6': '^',
	'7': '&', '8': '*', '9': '(', '0': ')', '-': '_', '=': '+', '[': '{',
	']': '}', '\\': '|', ';': ':', '\'': '"', ',': '<', '.': '>', '/': '?',
}

// Physical returns the key as it is on a US layout, e.g. "t" for the key
// typing "א" on a Hebrew one, so shortcuts work whatever the layout. It's
// meant for shortcuts, not for typing text. Keys are left as they are when
// the terminal doesn't report where they are, e.g. without the Kitty
// keyboard protocol, or when they're already where they'd be on a US layout.
func Physical(msg tea.KeyPressMsg) tea.KeyPressMsg {
	k := msg.Key()
	if k.BaseCode == 0 || k.BaseCode == k.Code || !unicode.IsPrint(k.BaseCode) {
		return msg
	}
	k.Code = k.BaseCode
	k.ShiftedCode = 0
	if k.Text != "" {
		text := k.BaseCode
		if k.Mod.Contains(tea.ModShift) {
			if s, ok := usShifted[text]; ok {
				text = s
			} else {
				text = unicode.ToUpper(text)
			}
		}
		k.Text = string(text)
	}
	return tea.KeyPressMsg(k)
}
