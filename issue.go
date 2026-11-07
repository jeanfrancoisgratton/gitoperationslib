package gitoperationslib

import (
	"context"
	"time"
)

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
	Number int `json:"number"`
	// IsPullRequest is set when Get or Edit found a pull request under this
	// number. Delete, and Edit with State, refuse those (ErrIsPullRequest).
	IsPullRequest bool       `json:"isPullRequest,omitzero"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	State         IssueState `json:"state"`
	Author        string     `json:"author"`
	Labels        []string   `json:"labels"`    // names
	Assignees     []string   `json:"assignees"` // logins
	Milestone     *Milestone `json:"milestone,omitzero"`
	Comments      int        `json:"comments"`
	Locked        bool       `json:"locked"`
	HTMLURL       string     `json:"htmlUrl"`
	CreatedAt     time.Time  `json:"createdAt,omitzero"`
	UpdatedAt     time.Time  `json:"updatedAt,omitzero"`
	ClosedAt      time.Time  `json:"closedAt,omitzero"`
}

// ListIssuesOptions selects issues to list. Pull requests are never
// included.
type ListIssuesOptions struct {
	ListOptions
	State IssueState `json:"state,omitzero"` // zero value: open
	// Labels selects issues carrying all these labels. A name that does not exist
	// selects nothing, on every forge.
	Labels []string `json:"labels,omitzero"`
}

// CreateIssueOptions describes an issue to open.
type CreateIssueOptions struct {
	Title string `json:"title"` // required
	Body  string `json:"body,omitzero"`
	// Labels are names, which must exist: anything else is a 404 *APIError, for
	// EditIssueOptions.Labels and Issues.AddLabels too.
	Labels    []string `json:"labels,omitzero"`
	Assignees []string `json:"assignees,omitzero"`
	Milestone int64    `json:"milestone,omitzero"` // Milestone.ID; 0 for none
}

// EditIssueOptions changes an issue. Nil fields are left unchanged; non-nil
// slices replace the current values (an empty slice clears them).
type EditIssueOptions struct {
	Title *string `json:"title,omitzero"`
	Body  *string `json:"body,omitzero"`
	// State closes or reopens an issue. On a pull request's number it returns
	// ErrIsPullRequest, after one more request.
	State     *IssueState `json:"state,omitzero"`
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

// Label is a repository's or an organisation's label.
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"` // six hex digits, lower case, without "#"
	Description string `json:"description"`
}

// EditLabelOptions changes a label. Nil fields are left unchanged.
type EditLabelOptions struct {
	Name        *string `json:"name,omitzero"` // renames the label; ErrNameTaken if in use
	Color       *string `json:"color,omitzero"`
	Description *string `json:"description,omitzero"`
}

// OrgLabels manages an organisation's labels, which all its repositories can use
// by name. Only the Forgejo backend implements it (CapOrgLabels): GitHub's
// organisation default labels only seed new repositories, and Bitbucket has no
// issues. Find it with a type assertion:
//
//	l, ok := client.(gitoperationslib.OrgLabels)
//
// They are addressed by Label.ID, as names can repeat.
type OrgLabels interface {
	// ListOrgLabels returns an empty page when org is a user, who has none, and
	// a 404 *APIError when it is neither.
	ListOrgLabels(ctx context.Context, org string, opts ListOptions) (Page[Label], error)
	GetOrgLabel(ctx context.Context, org string, id int64) (*Label, error)
	// CreateOrgLabel returns ErrNameTaken if the organisation has that name.
	CreateOrgLabel(ctx context.Context, org string, label Label) (*Label, error)
	EditOrgLabel(ctx context.Context, org string, id int64, opts EditLabelOptions) (*Label, error)
	DeleteOrgLabel(ctx context.Context, org string, id int64) error
}

// LabelsByID addresses a repository's labels by Label.ID, to repair names that
// several labels share (ErrAmbiguousName), which Forgejo allows. Only the Forgejo
// backend implements it (CapLabelsByID), as GitHub refuses such names. Find it with
// a type assertion:
//
//	l, ok := client.(gitoperationslib.LabelsByID)
type LabelsByID interface {
	EditLabelByID(ctx context.Context, owner, repo string, id int64, opts EditLabelOptions) (*Label, error)
	DeleteLabelByID(ctx context.Context, owner, repo string, id int64) error
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
	ID          int64          `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	State       MilestoneState `json:"state"`
	// DueOn is zero if none. GitHub stores a date, not a time: whatever was sent, it
	// answers midnight UTC of the day as written in the request, which local time
	// west of Greenwich reads as the day before.
	DueOn        time.Time `json:"dueOn,omitzero"`
	OpenIssues   int       `json:"openIssues"`
	ClosedIssues int       `json:"closedIssues"`
	ClosedAt     time.Time `json:"closedAt,omitzero"`
}

// CreateMilestoneOptions describes a milestone to create.
type CreateMilestoneOptions struct {
	Title       string    `json:"title"` // required; ErrNameTaken if in use
	Description string    `json:"description,omitzero"`
	DueOn       time.Time `json:"dueOn,omitzero"` // zero: none
}

// EditMilestoneOptions changes a milestone. Nil fields are left unchanged.
type EditMilestoneOptions struct {
	Title       *string         `json:"title,omitzero"` // ErrNameTaken if in use
	Description *string         `json:"description,omitzero"`
	State       *MilestoneState `json:"state,omitzero"` // close or reopen
	DueOn       *time.Time      `json:"dueOn,omitzero"`
	// ClearDueOn removes the due date, and cannot go with DueOn. It returns
	// ErrNotSupported on Forgejo, whose API cannot remove one.
	ClearDueOn bool `json:"clearDueOn,omitzero"`
}

// IssueLocks locks and unlocks an issue's conversation. The Forgejo backend
// (version 17; ErrNotSupported on older servers) and the GitHub backend implement
// it (CapIssueLocks). Find it with a type assertion:
//
//	l, ok := client.(gitoperationslib.IssueLocks)
//
// Issue.Locked reports the state. Locking a locked issue and unlocking an unlocked
// one succeed, changing nothing.
type IssueLocks interface {
	// LockIssue locks the issue. Forgejo requires a reason, and takes any text;
	// GitHub accepts no reason, or one of the LockReason constants. GitHub also
	// locks pull requests by number, while Forgejo answers HTTP 422.
	LockIssue(ctx context.Context, owner, repo string, number int, reason LockReason) error
	UnlockIssue(ctx context.Context, owner, repo string, number int) error
}

// LockReason says why an issue was locked.
type LockReason string

const (
	LockReasonOffTopic  LockReason = "off-topic"
	LockReasonTooHeated LockReason = "too heated"
	LockReasonResolved  LockReason = "resolved"
	LockReasonSpam      LockReason = "spam"
)
