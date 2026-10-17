package gitoperationslib

import "time"

// PRState is the state of a pull request. PRStateAll is only meaningful as a
// List filter.
type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed" // includes merged pull requests
	PRStateAll    PRState = "all"
)

// PRBranch is one side of a pull request.
type PRBranch struct {
	Ref  string // branch name
	SHA  string
	Repo string // "owner/name"; differs from the base repository for forks
}

// PullRequest is a pull request.
type PullRequest struct {
	Number int
	// Title is returned as the server stores it. On Forgejo a draft is a
	// title starting with a work-in-progress prefix ("WIP:" or "[WIP]" by
	// default), which therefore appears in Title.
	Title  string
	Body   string
	State  PRState
	Draft  bool
	Author string
	Head   PRBranch
	Base   PRBranch
	// Mergeable is nil while the server has not computed it yet (GitHub
	// computes it in the background).
	Mergeable      *bool
	Merged         bool
	MergedBy       string
	MergedAt       time.Time
	MergeCommitSHA string
	HTMLURL        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ClosedAt       time.Time
}

// ListPROptions selects pull requests to list.
type ListPROptions struct {
	ListOptions
	State PRState // zero value: open
}

// CreatePROptions describes a pull request to open.
type CreatePROptions struct {
	Title string // required
	Body  string
	Head  string // branch with the changes; "user:branch" for a fork
	Base  string // branch to merge into
	// Draft opens the pull request as a draft. On Forgejo this adds the
	// "WIP: " prefix to Title unless it already has a draft prefix.
	Draft               bool
	AllowMaintainerEdit *bool // let base maintainers push to the head branch
}

// EditPROptions changes a pull request. Nil fields are left unchanged.
type EditPROptions struct {
	Title *string
	Body  *string
	Base  *string
	// State closes (PRStateClosed) or reopens (PRStateOpen) the pull request.
	State *PRState
	// Draft converts to or from a draft. Forgejo adds or removes the title
	// prefix; GitHub cannot do it through its REST API (ErrNotSupported).
	Draft               *bool
	AllowMaintainerEdit *bool
}

// MergeOptions controls a merge.
type MergeOptions struct {
	// Method defaults to MergeMethodMerge. GitHub supports only merge,
	// squash and rebase.
	Method  MergeMethod
	Title   string // merge or squash commit title; empty: server default
	Message string // commit message body; empty: server default
	// HeadSHA makes the merge fail unless the head branch is still at this
	// commit.
	HeadSHA string
	// DeleteBranch deletes the head branch after a successful merge.
	DeleteBranch bool
	// WhenChecksSucceed schedules the merge for when status checks pass
	// (Forgejo only).
	WhenChecksSucceed bool
}

// ReviewState is the outcome of a review.
type ReviewState string

const (
	ReviewApproved         ReviewState = "approved"
	ReviewChangesRequested ReviewState = "changes_requested"
	ReviewCommented        ReviewState = "commented"
	ReviewPending          ReviewState = "pending"   // started, not submitted
	ReviewDismissed        ReviewState = "dismissed" // GitHub
	ReviewRequested        ReviewState = "requested" // Forgejo: review requested, not given
)

// ReviewEvent is the verdict submitted with a review.
type ReviewEvent string

const (
	ReviewEventApprove        ReviewEvent = "approve"
	ReviewEventRequestChanges ReviewEvent = "request_changes"
	ReviewEventComment        ReviewEvent = "comment"
)

// Review is a pull request review.
type Review struct {
	ID          int64
	Reviewer    string
	State       ReviewState
	Body        string
	CommitID    string
	HTMLURL     string
	SubmittedAt time.Time
}

// CreateReviewOptions submits a review. Authors cannot approve or request
// changes on their own pull requests.
type CreateReviewOptions struct {
	Event    ReviewEvent // required
	Body     string      // required for request_changes and comment
	CommitID string      // empty: the current head
}
