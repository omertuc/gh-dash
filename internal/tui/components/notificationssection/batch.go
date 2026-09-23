package notificationssection

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// maxConcurrentNotificationFetches bounds how many notification subjects
	// are fetched from GitHub at once.
	maxConcurrentNotificationFetches = 8
	// notificationUpdatesBatchWindow is how long to wait for more results after
	// the first one arrives, so that many updates cost a single redraw.
	notificationUpdatesBatchWindow = 100 * time.Millisecond
)

// NotificationUpdatesBatchMsg carries the results of several background
// notification fetches (e.g. UpdateNotificationCommentsMsg), delivered
// together so they don't each trigger a separate update and redraw.
type NotificationUpdatesBatchMsg struct {
	Updates []tea.Msg
	next    tea.Cmd
}

// Next returns the command that waits for the next batch, or nil once all
// fetches have completed. It must be run exactly once per batch, regardless
// of how many sections the batch is delivered to.
func (msg NotificationUpdatesBatchMsg) Next() tea.Cmd {
	return msg.next
}

// batchNotificationUpdates runs the given fetch commands with bounded
// concurrency and delivers their results as NotificationUpdatesBatchMsgs.
func batchNotificationUpdates(fetches []tea.Cmd) tea.Cmd {
	if len(fetches) == 0 {
		return nil
	}

	return func() tea.Msg {
		results := make(chan tea.Msg)
		jobs := make(chan tea.Cmd)

		var wg sync.WaitGroup
		for range min(maxConcurrentNotificationFetches, len(fetches)) {
			wg.Go(func() {
				for fetch := range jobs {
					if msg := fetch(); msg != nil {
						results <- msg
					}
				}
			})
		}
		go func() {
			for _, fetch := range fetches {
				jobs <- fetch
			}
			close(jobs)
			wg.Wait()
			close(results)
		}()

		return waitForNotificationUpdates(results)()
	}
}

// waitForNotificationUpdates blocks until at least one result is available,
// then collects whatever else arrives within the batch window.
func waitForNotificationUpdates(results <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		first, ok := <-results
		if !ok {
			return nil
		}

		batch := NotificationUpdatesBatchMsg{
			Updates: []tea.Msg{first},
			next:    waitForNotificationUpdates(results),
		}
		timeout := time.After(notificationUpdatesBatchWindow)
		for {
			select {
			case msg, ok := <-results:
				if !ok {
					batch.next = nil
					return batch
				}
				batch.Updates = append(batch.Updates, msg)
			case <-timeout:
				return batch
			}
		}
	}
}
