package keys

import (
	"cmp"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
)

// HintKeyPair lists the keys of an up/down pair, sharing a common
// modifier to save space, e.g. "Ctrl+u/d" rather than "Ctrl+u Ctrl+d".
func HintKeyPair(up, down key.Binding) string {
	upName, downName := HintKeys(up), HintKeys(down)
	i := strings.LastIndex(upName, "+")
	if len(up.Keys()) == 1 && len(down.Keys()) == 1 && i > 0 &&
		strings.HasPrefix(downName, upName[:i+1]) {
		return upName + "/" + downName[i+1:]
	}
	return upName + " " + downName
}

// HintKeys lists all of a binding's keys, e.g. "k/↑".
func HintKeys(b key.Binding) string {
	var names []string
	for _, k := range b.Keys() {
		names = append(names, keyName(k))
	}
	// Show plain characters first since they're what people usually type,
	// then symbols like arrows, then named keys
	slices.SortStableFunc(names, func(a, b string) int {
		return cmp.Compare(keyNameRank(a), keyNameRank(b))
	})
	return strings.Join(names, "/")
}

func keyNameRank(name string) int {
	switch {
	case len(name) == 1:
		return 0
	case len([]rune(name)) == 1:
		return 1
	}
	return 2
}

var specialKeyNames = map[string]string{
	"up":     "↑",
	"down":   "↓",
	"left":   "←",
	"right":  "→",
	"pgup":   "PgUp",
	"pgdown": "PgDn",
}

// keyName formats a key for display, e.g. "ctrl+d" as "Ctrl+d" and "up" as "↑".
func keyName(k string) string {
	if name, ok := specialKeyNames[k]; ok {
		return name
	}
	parts := strings.Split(k, "+")
	for i, part := range parts {
		if len([]rune(part)) > 1 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "+")
}
