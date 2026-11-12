package gitoperationslib

import (
	"context"
	"time"
)

// Issues manages issues and their comments. Label and comment methods also
// accept pull request numbers. Lock and Unlock need CapIssueLocks (Forgejo 17,
// GitHub); Issue.Locked reports the state, and locking a locked issue or
// unlocking an unlocked one changes nothing.
type Issues interface {
	List(ctx context.Context, owner, repo string, opts ListIssuesOptions) (Page[Issue], error)
	Get(ctx context.Context, owner, repo string, number int) (*Issue, error)
	Create(ctx context.Context, owner, repo string, opts CreateIssueOptions) (*Issue, error)
	Edit(ctx context.Context, owner, repo string, number int, opts EditIssueOptions) (*Issue, error)
	// Delete removes an issue permanently (repository admins only; GitHub
	// through GraphQL).
	Delete(ctx context.Context, owner, repo string, number int) error

	AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error
	RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error

	ListComments(ctx context.Context, owner, repo string, number int, opts ListOptions) (Page[Comment], error)
	CreateComment(ctx context.Context, owner, repo string, number int, body string) (*Comment, error)
	EditComment(ctx context.Context, owner, repo string, id int64, body string) (*Comment, error)
	DeleteComment(ctx context.Context, owner, repo string, id int64) error

	// Lock locks the issue. Forgejo requires a reason, and takes any text;
	// GitHub accepts no reason, or one of the LockReason constants. GitHub also
	// locks pull requests by number, while Forgejo answers HTTP 422.
	Lock(ctx context.Context, owner, repo string, number int, reason LockReason) error
	Unlock(ctx context.Context, owner, repo string, number int) error
}

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

// LockReason says why an issue was locked.
type LockReason string

const (
	LockReasonOffTopic  LockReason = "off-topic"
	LockReasonTooHeated LockReason = "too heated"
	LockReasonResolved  LockReason = "resolved"
	LockReasonSpam      LockReason = "spam"
)
