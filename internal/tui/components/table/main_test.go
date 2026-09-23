package table

import (
	"os"
	"testing"

	zone "github.com/lrstanley/bubblezone/v2"
)

func TestMain(m *testing.M) {
	// Views mark mouse zones, which needs the global manager. It's disabled
	// so views render without the zone markers.
	zone.NewGlobal()
	zone.SetEnabled(false)
	os.Exit(m.Run())
}
