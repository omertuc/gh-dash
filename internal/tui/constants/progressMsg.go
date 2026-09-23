package constants

import tea "charm.land/bubbletea/v2"

type TaskFinishedMsg struct {
	TaskId      string
	SectionId   int
	SectionType string
	Err         error
	Msg         tea.Msg
}

// TaskStartedMsg carries a task's optimistic update, e.g. a comment being
// posted, applied as soon as the task starts
type TaskStartedMsg struct {
	SectionId   int
	SectionType string
	Msg         tea.Msg
}

type ClearTaskMsg struct {
	TaskId string
}
