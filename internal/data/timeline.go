package data

import "time"

// TimelineItems are the events on a PR's timeline other than comments and
// reviews, which are fetched separately, e.g. pushed commits, force pushes,
// references from other issues and label changes
type TimelineItems struct {
	Nodes []TimelineItem
}

type TimelineActor struct {
	Login string
}

// TimelineEvent holds the fields most events have
type TimelineEvent struct {
	Actor     TimelineActor
	CreatedAt time.Time
}

type TimelineCommit struct {
	AbbreviatedOid  string
	MessageHeadline string
	CommittedDate   time.Time
	Author          struct {
		Name string
		User struct {
			Login string
		}
	}
}

type TimelineShortCommit struct {
	AbbreviatedOid string
}

// TimelineSubject is an issue or PR that references this PR
type TimelineSubject struct {
	Number     int
	Title      string
	State      string
	Url        string
	Repository struct {
		NameWithOwner string
	}
}

type TimelineAssignee struct {
	Actor TimelineActor `graphql:"... on Actor"`
}

type TimelineReviewer struct {
	Typename string        `graphql:"__typename"`
	Actor    TimelineActor `graphql:"... on Actor"`
	Team     struct {
		Name string
	} `graphql:"... on Team"`
}

// TimelineItem is one of the event types below, which Typename tells apart
type TimelineItem struct {
	Typename string `graphql:"__typename"`

	PullRequestCommit struct {
		Commit TimelineCommit
	} `graphql:"... on PullRequestCommit"`
	HeadRefForcePushedEvent struct {
		TimelineEvent
		BeforeCommit TimelineShortCommit
		AfterCommit  TimelineShortCommit
	} `graphql:"... on HeadRefForcePushedEvent"`
	BaseRefForcePushedEvent struct {
		TimelineEvent
		BeforeCommit TimelineShortCommit
		AfterCommit  TimelineShortCommit
	} `graphql:"... on BaseRefForcePushedEvent"`
	BaseRefChangedEvent struct {
		TimelineEvent
		PreviousRefName string
		CurrentRefName  string
	} `graphql:"... on BaseRefChangedEvent"`
	CrossReferencedEvent struct {
		TimelineEvent
		WillCloseTarget bool
		Source          struct {
			Typename    string          `graphql:"__typename"`
			Issue       TimelineSubject `graphql:"... on Issue"`
			PullRequest TimelineSubject `graphql:"... on PullRequest"`
		}
	} `graphql:"... on CrossReferencedEvent"`
	ReferencedEvent struct {
		TimelineEvent
		Commit struct {
			AbbreviatedOid  string
			MessageHeadline string
		}
		CommitRepository struct {
			NameWithOwner string
		}
	} `graphql:"... on ReferencedEvent"`
	LabeledEvent struct {
		TimelineEvent
		Label Label
	} `graphql:"... on LabeledEvent"`
	UnlabeledEvent struct {
		TimelineEvent
		Label Label
	} `graphql:"... on UnlabeledEvent"`
	AssignedEvent struct {
		TimelineEvent
		Assignee TimelineAssignee
	} `graphql:"... on AssignedEvent"`
	UnassignedEvent struct {
		TimelineEvent
		Assignee TimelineAssignee
	} `graphql:"... on UnassignedEvent"`
	ReviewRequestedEvent struct {
		TimelineEvent
		RequestedReviewer TimelineReviewer
	} `graphql:"... on ReviewRequestedEvent"`
	ReviewRequestRemovedEvent struct {
		TimelineEvent
		RequestedReviewer TimelineReviewer
	} `graphql:"... on ReviewRequestRemovedEvent"`
	ReviewDismissedEvent struct {
		TimelineEvent
		Review struct {
			Author TimelineActor
		}
	} `graphql:"... on ReviewDismissedEvent"`
	RenamedTitleEvent struct {
		TimelineEvent
		PreviousTitle string
		CurrentTitle  string
	} `graphql:"... on RenamedTitleEvent"`
	MilestonedEvent struct {
		TimelineEvent
		MilestoneTitle string
	} `graphql:"... on MilestonedEvent"`
	DemilestonedEvent struct {
		TimelineEvent
		MilestoneTitle string
	} `graphql:"... on DemilestonedEvent"`
	MergedEvent struct {
		TimelineEvent
		MergeRefName string
		Commit       TimelineShortCommit
	} `graphql:"... on MergedEvent"`
	ClosedEvent                TimelineEvent `graphql:"... on ClosedEvent"`
	ReopenedEvent              TimelineEvent `graphql:"... on ReopenedEvent"`
	ReadyForReviewEvent        TimelineEvent `graphql:"... on ReadyForReviewEvent"`
	ConvertToDraftEvent        TimelineEvent `graphql:"... on ConvertToDraftEvent"`
	HeadRefDeletedEvent        TimelineEvent `graphql:"... on HeadRefDeletedEvent"`
	HeadRefRestoredEvent       TimelineEvent `graphql:"... on HeadRefRestoredEvent"`
	AutoMergeEnabledEvent      TimelineEvent `graphql:"... on AutoMergeEnabledEvent"`
	AutoMergeDisabledEvent     TimelineEvent `graphql:"... on AutoMergeDisabledEvent"`
	AddedToMergeQueueEvent     TimelineEvent `graphql:"... on AddedToMergeQueueEvent"`
	RemovedFromMergeQueueEvent TimelineEvent `graphql:"... on RemovedFromMergeQueueEvent"`
	LockedEvent                TimelineEvent `graphql:"... on LockedEvent"`
	UnlockedEvent              TimelineEvent `graphql:"... on UnlockedEvent"`
}

// Event returns the actor of the item and when it happened. Commits have no
// actor and use their commit date.
func (item TimelineItem) Event() TimelineEvent {
	switch item.Typename {
	case "PullRequestCommit":
		return TimelineEvent{CreatedAt: item.PullRequestCommit.Commit.CommittedDate}
	case "HeadRefForcePushedEvent":
		return item.HeadRefForcePushedEvent.TimelineEvent
	case "BaseRefForcePushedEvent":
		return item.BaseRefForcePushedEvent.TimelineEvent
	case "BaseRefChangedEvent":
		return item.BaseRefChangedEvent.TimelineEvent
	case "CrossReferencedEvent":
		return item.CrossReferencedEvent.TimelineEvent
	case "ReferencedEvent":
		return item.ReferencedEvent.TimelineEvent
	case "LabeledEvent":
		return item.LabeledEvent.TimelineEvent
	case "UnlabeledEvent":
		return item.UnlabeledEvent.TimelineEvent
	case "AssignedEvent":
		return item.AssignedEvent.TimelineEvent
	case "UnassignedEvent":
		return item.UnassignedEvent.TimelineEvent
	case "ReviewRequestedEvent":
		return item.ReviewRequestedEvent.TimelineEvent
	case "ReviewRequestRemovedEvent":
		return item.ReviewRequestRemovedEvent.TimelineEvent
	case "ReviewDismissedEvent":
		return item.ReviewDismissedEvent.TimelineEvent
	case "RenamedTitleEvent":
		return item.RenamedTitleEvent.TimelineEvent
	case "MilestonedEvent":
		return item.MilestonedEvent.TimelineEvent
	case "DemilestonedEvent":
		return item.DemilestonedEvent.TimelineEvent
	case "MergedEvent":
		return item.MergedEvent.TimelineEvent
	case "ClosedEvent":
		return item.ClosedEvent
	case "ReopenedEvent":
		return item.ReopenedEvent
	case "ReadyForReviewEvent":
		return item.ReadyForReviewEvent
	case "ConvertToDraftEvent":
		return item.ConvertToDraftEvent
	case "HeadRefDeletedEvent":
		return item.HeadRefDeletedEvent
	case "HeadRefRestoredEvent":
		return item.HeadRefRestoredEvent
	case "AutoMergeEnabledEvent":
		return item.AutoMergeEnabledEvent
	case "AutoMergeDisabledEvent":
		return item.AutoMergeDisabledEvent
	case "AddedToMergeQueueEvent":
		return item.AddedToMergeQueueEvent
	case "RemovedFromMergeQueueEvent":
		return item.RemovedFromMergeQueueEvent
	case "LockedEvent":
		return item.LockedEvent
	case "UnlockedEvent":
		return item.UnlockedEvent
	}
	return TimelineEvent{}
}
