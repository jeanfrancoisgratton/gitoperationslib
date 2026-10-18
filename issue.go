package gitoperationslib

import "time"

// IssueState is the state of an issue. IssueStateAll is only meaningful as a
// List filter.
type IssueState string

const (
	IssueStateOpen   IssueState = "open"
	IssueStateClosed IssueState = "closed"
	IssueStateAll    IssueState = "all"
)

// Issue is an issue. Issues and pull requests share one number sequence;
// label, assignee and milestone methods also work on pull requests.
type Issue struct {
	Number    int
	Title     string
	Body      string
	State     IssueState
	Author    string
	Labels    []string // names
	Assignees []string // logins
	Milestone *Milestone
	Comments  int
	Locked    bool
	HTMLURL   string
	CreatedAt time.Time
	UpdatedAt time.Time
	ClosedAt  time.Time
}

// ListIssuesOptions selects issues to list. Pull requests are never
// included.
type ListIssuesOptions struct {
	ListOptions
	State  IssueState // zero value: open
	Labels []string   // issues carrying all these labels
}

// CreateIssueOptions describes an issue to open.
type CreateIssueOptions struct {
	Title     string // required
	Body      string
	Labels    []string // names; they must exist
	Assignees []string
	Milestone int64 // Milestone.ID; 0 for none
}

// EditIssueOptions changes an issue. Nil fields are left unchanged; non-nil
// slices replace the current values (an empty slice clears them).
type EditIssueOptions struct {
	Title     *string
	Body      *string
	State     *IssueState // close or reopen
	Labels    *[]string
	Assignees *[]string
	Milestone *int64 // 0 removes the milestone
}

// Comment is a comment on an issue or pull request.
type Comment struct {
	ID        int64
	Author    string
	Body      string
	HTMLURL   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Label is a repository label.
type Label struct {
	ID          int64
	Name        string
	Color       string // six hex digits, lower case, without "#"
	Description string
}

// EditLabelOptions changes a label. Nil fields are left unchanged.
type EditLabelOptions struct {
	Name        *string // renames the label
	Color       *string
	Description *string
}

// MilestoneState is the state of a milestone; MilestoneStateAll is only
// meaningful as a List filter.
type MilestoneState string

const (
	MilestoneStateOpen   MilestoneState = "open"
	MilestoneStateClosed MilestoneState = "closed"
	MilestoneStateAll    MilestoneState = "all"
)

// Milestone is a repository milestone.
type Milestone struct {
	// ID addresses the milestone: Forgejo's ID, GitHub's milestone number.
	ID           int64
	Title        string
	Description  string
	State        MilestoneState
	DueOn        time.Time // zero if none
	OpenIssues   int
	ClosedIssues int
	ClosedAt     time.Time
}

// CreateMilestoneOptions describes a milestone to create.
type CreateMilestoneOptions struct {
	Title       string // required
	Description string
	DueOn       time.Time // zero: none
}

// EditMilestoneOptions changes a milestone. Nil fields are left unchanged.
type EditMilestoneOptions struct {
	Title       *string
	Description *string
	State       *MilestoneState // close or reopen
	DueOn       *time.Time
}
