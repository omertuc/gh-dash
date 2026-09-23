package notificationssection

import (
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// drainBatches runs cmd and every follow-up Next() command, like the UI does,
// returning all delivered updates and the number of batches.
func drainBatches(t *testing.T, cmd tea.Cmd) ([]tea.Msg, int) {
	t.Helper()
	var updates []tea.Msg
	batches := 0
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		batch, ok := msg.(NotificationUpdatesBatchMsg)
		if !ok {
			t.Fatalf("expected NotificationUpdatesBatchMsg, got %T", msg)
		}
		batches++
		updates = append(updates, batch.Updates...)
		cmd = batch.Next()
	}
	return updates, batches
}

func TestBatchNotificationUpdatesDeliversAllResults(t *testing.T) {
	var fetches []tea.Cmd
	for i := range 20 {
		fetches = append(fetches, func() tea.Msg {
			if i%5 == 0 {
				return nil // failed fetches produce no update
			}
			return UpdateNotificationUrlMsg{Id: string(rune('a' + i))}
		})
	}

	updates, batches := drainBatches(t, batchNotificationUpdates(fetches))

	if len(updates) != 16 {
		t.Fatalf("expected 16 updates, got %d", len(updates))
	}
	seen := map[string]bool{}
	for _, u := range updates {
		seen[u.(UpdateNotificationUrlMsg).Id] = true
	}
	if len(seen) != 16 {
		t.Fatalf("expected 16 distinct updates, got %d", len(seen))
	}
	if batches >= 16 {
		t.Fatalf("expected fast results to be batched, got %d batches", batches)
	}
}

func TestBatchNotificationUpdatesSplitsSlowResults(t *testing.T) {
	fetches := []tea.Cmd{
		func() tea.Msg { return UpdateNotificationUrlMsg{Id: "fast"} },
		func() tea.Msg {
			time.Sleep(3 * notificationUpdatesBatchWindow)
			return UpdateNotificationUrlMsg{Id: "slow"}
		},
	}

	updates, batches := drainBatches(t, batchNotificationUpdates(fetches))

	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if batches != 2 {
		t.Fatalf("expected the slow result in its own batch, got %d batches", batches)
	}
}

func TestBatchNotificationUpdatesBoundsConcurrency(t *testing.T) {
	var running, maxRunning atomic.Int32
	var fetches []tea.Cmd
	for range 3 * maxConcurrentNotificationFetches {
		fetches = append(fetches, func() tea.Msg {
			n := running.Add(1)
			for {
				m := maxRunning.Load()
				if n <= m || maxRunning.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			running.Add(-1)
			return UpdateNotificationUrlMsg{}
		})
	}

	drainBatches(t, batchNotificationUpdates(fetches))

	if got := maxRunning.Load(); got > maxConcurrentNotificationFetches {
		t.Fatalf("expected at most %d concurrent fetches, got %d",
			maxConcurrentNotificationFetches, got)
	}
}

func TestBatchNotificationUpdatesAllFailed(t *testing.T) {
	fetches := []tea.Cmd{func() tea.Msg { return nil }}
	updates, batches := drainBatches(t, batchNotificationUpdates(fetches))
	if len(updates) != 0 || batches != 0 {
		t.Fatalf("expected no batches, got %d batches with %d updates", batches, len(updates))
	}
	if batchNotificationUpdates(nil) != nil {
		t.Fatal("expected nil command for no fetches")
	}
}
