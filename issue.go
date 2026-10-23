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
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     IssueState `json:"state"`
	Author    string     `json:"author"`
	Labels    []string   `json:"labels"`    // names
	Assignees []string   `json:"assignees"` // logins
	Milestone *Milestone `json:"milestone,omitzero"`
	Comments  int        `json:"comments"`
	Locked    bool       `json:"locked"`
	HTMLURL   string     `json:"htmlUrl"`
	CreatedAt time.Time  `json:"createdAt,omitzero"`
	UpdatedAt time.Time  `json:"updatedAt,omitzero"`
	ClosedAt  time.Time  `json:"closedAt,omitzero"`
}

// ListIssuesOptions selects issues to list. Pull requests are never
// included.
type ListIssuesOptions struct {
	ListOptions
	State  IssueState `json:"state,omitzero"`  // zero value: open
	Labels []string   `json:"labels,omitzero"` // issues carrying all these labels
}

// CreateIssueOptions describes an issue to open.
type CreateIssueOptions struct {
	Title     string   `json:"title"` // required
	Body      string   `json:"body,omitzero"`
	Labels    []string `json:"labels,omitzero"` // names; they must exist
	Assignees []string `json:"assignees,omitzero"`
	Milestone int64    `json:"milestone,omitzero"` // Milestone.ID; 0 for none
}

// EditIssueOptions changes an issue. Nil fields are left unchanged; non-nil
// slices replace the current values (an empty slice clears them).
type EditIssueOptions struct {
	Title     *string     `json:"title,omitzero"`
	Body      *string     `json:"body,omitzero"`
	State     *IssueState `json:"state,omitzero"` // close or reopen
	Labels    *[]string   `json:"labels,omitzero"`
	Assignees *[]string   `json:"assignees,omitzero"`
	Milestone *int64      `json:"milestone,omitzero"` // 0 removes the milestone
}

// Comment is a comment on an issue or pull request.
type Comment struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"htmlUrl"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// Label is a repository label.
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"` // six hex digits, lower case, without "#"
	Description string `json:"description"`
}

// EditLabelOptions changes a label. Nil fields are left unchanged.
type EditLabelOptions struct {
	Name        *string `json:"name,omitzero"` // renames the label
	Color       *string `json:"color,omitzero"`
	Description *string `json:"description,omitzero"`
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
	ID           int64          `json:"id"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	State        MilestoneState `json:"state"`
	DueOn        time.Time      `json:"dueOn,omitzero"` // zero if none
	OpenIssues   int            `json:"openIssues"`
	ClosedIssues int            `json:"closedIssues"`
	ClosedAt     time.Time      `json:"closedAt,omitzero"`
}

// CreateMilestoneOptions describes a milestone to create.
type CreateMilestoneOptions struct {
	Title       string    `json:"title"` // required
	Description string    `json:"description,omitzero"`
	DueOn       time.Time `json:"dueOn,omitzero"` // zero: none
}

// EditMilestoneOptions changes a milestone. Nil fields are left unchanged.
type EditMilestoneOptions struct {
	Title       *string         `json:"title,omitzero"`
	Description *string         `json:"description,omitzero"`
	State       *MilestoneState `json:"state,omitzero"` // close or reopen
	DueOn       *time.Time      `json:"dueOn,omitzero"`
}
