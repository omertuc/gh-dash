package keys

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// decode decodes a key as the terminal reports it.
func decode(t *testing.T, seq string) tea.KeyPressMsg {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(seq))
	require.Equal(t, len(seq), n)
	k, ok := ev.(uv.KeyPressEvent)
	require.True(t, ok, "got %T", ev)
	return tea.KeyPressMsg(k)
}

func TestPhysicalGoesByWhereTheKeyIsOnAUSLayout(t *testing.T) {
	for _, tc := range []struct {
		name, seq, typed, physical string
	}{
		// The key code, the shifted key and where it is on a US layout,
		// the modifiers, and the text it types
		{"hebrew t", "\x1b[1488::116;1;1488u", "א", "t"},
		{"hebrew .", "\x1b[1509::46;1;1509u", "ץ", "."},
		{"hebrew / types .", "\x1b[46::47;1;46u", ".", "/"},
		{"hebrew shift+.", "\x1b[1509::46;2;62u", ">", ">"},
		{"hebrew shift+d", "\x1b[1490::100;2;68u", "D", "D"},
		// Bubble Tea already goes by the key for shortcuts with modifiers
		{"hebrew ctrl+c", "\x1b[1489::99;5u", "ctrl+c", "ctrl+c"},
		{"french azerty a types q", "\x1b[113::97;1;113u", "q", "a"},
		// Keys already where they'd be on a US layout aren't reported twice
		{"us t", "\x1b[116;1;116u", "t", "t"},
		{"us enter", "\x1b[13u", "enter", "enter"},
		// Without the Kitty keyboard protocol, there's only the text
		{"legacy hebrew", "א", "א", "א"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := decode(t, tc.seq)
			require.Equal(t, tc.typed, msg.String())
			require.Equal(t, tc.physical, Physical(msg).String())
		})
	}
}

func TestPhysicalMatchesBindings(t *testing.T) {
	require.True(t, key.Matches(Physical(decode(t, "\x1b[1500::106;1;1500u")), Keys.Down),
		"the key j is on moves down")
	require.True(t, key.Matches(Physical(decode(t, "\x1b[1509::46;1;1509u")), NotificationKeys.CheckCommands))
}
