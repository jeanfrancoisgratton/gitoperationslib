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
	Ref  string `json:"ref"` // branch name
	SHA  string `json:"sha"`
	Repo string `json:"repo"` // "owner/name"; differs from the base repository for forks
}

// PullRequest is a pull request.
type PullRequest struct {
	Number int `json:"number"`
	// Title is returned as the server stores it. On Forgejo a draft is a
	// title starting with a work-in-progress prefix ("WIP:" or "[WIP]" by
	// default), which therefore appears in Title.
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	State  PRState  `json:"state"`
	Draft  bool     `json:"draft"`
	Author string   `json:"author"`
	Head   PRBranch `json:"head"`
	Base   PRBranch `json:"base"`
	// Mergeable is nil while the server has not computed it yet (GitHub
	// computes it in the background).
	Mergeable      *bool     `json:"mergeable,omitzero"`
	Merged         bool      `json:"merged"`
	MergedBy       string    `json:"mergedBy,omitzero"`
	MergedAt       time.Time `json:"mergedAt,omitzero"`
	MergeCommitSHA string    `json:"mergeCommitSha,omitzero"`
	HTMLURL        string    `json:"htmlUrl"`
	CreatedAt      time.Time `json:"createdAt,omitzero"`
	UpdatedAt      time.Time `json:"updatedAt,omitzero"`
	ClosedAt       time.Time `json:"closedAt,omitzero"`
}

// ListPROptions selects pull requests to list.
type ListPROptions struct {
	ListOptions
	State PRState `json:"state,omitzero"` // zero value: open
}

// CreatePROptions describes a pull request to open.
type CreatePROptions struct {
	Title string `json:"title"` // required
	Body  string `json:"body,omitzero"`
	Head  string `json:"head"` // branch with the changes; "user:branch" for a fork
	Base  string `json:"base"` // branch to merge into
	// Draft opens the pull request as a draft. On Forgejo this adds the
	// "WIP: " prefix to Title unless it already has a draft prefix.
	Draft               bool  `json:"draft,omitzero"`
	AllowMaintainerEdit *bool `json:"allowMaintainerEdit,omitzero"` // let base maintainers push to the head branch
}

// EditPROptions changes a pull request. Nil fields are left unchanged.
type EditPROptions struct {
	Title *string `json:"title,omitzero"`
	Body  *string `json:"body,omitzero"`
	Base  *string `json:"base,omitzero"`
	// State closes (PRStateClosed) or reopens (PRStateOpen) the pull request.
	State *PRState `json:"state,omitzero"`
	// Draft converts to or from a draft. Forgejo adds or removes the title
	// prefix; GitHub uses its GraphQL API.
	Draft               *bool `json:"draft,omitzero"`
	AllowMaintainerEdit *bool `json:"allowMaintainerEdit,omitzero"`
}

// MergeOptions controls a merge.
type MergeOptions struct {
	// Method defaults to MergeMethodMerge. GitHub supports only merge,
	// squash and rebase.
	Method  MergeMethod `json:"method,omitzero"`
	Title   string      `json:"title,omitzero"`   // merge or squash commit title; empty: server default
	Message string      `json:"message,omitzero"` // commit message body; empty: server default
	// HeadSHA makes the merge fail unless the head branch is still at this
	// commit.
	HeadSHA string `json:"headSha,omitzero"`
	// DeleteBranch deletes the head branch after a successful merge.
	DeleteBranch bool `json:"deleteBranch,omitzero"`
	// WhenChecksSucceed schedules the merge for when status checks pass;
	// cancel it with CancelAutoMerge. GitHub (through GraphQL) accepts it
	// only in repositories that allow auto-merge, while the pull request
	// still waits for a required check, and not together with DeleteBranch.
	WhenChecksSucceed bool `json:"whenChecksSucceed,omitzero"`
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
	ID          int64       `json:"id"`
	Reviewer    string      `json:"reviewer"`
	State       ReviewState `json:"state"`
	Body        string      `json:"body"`
	CommitID    string      `json:"commitId"`
	HTMLURL     string      `json:"htmlUrl"`
	SubmittedAt time.Time   `json:"submittedAt,omitzero"`
}

// CreateReviewOptions submits a review. Authors cannot approve or request
// changes on their own pull requests.
type CreateReviewOptions struct {
	Event    ReviewEvent `json:"event"`             // required
	Body     string      `json:"body,omitzero"`     // required for request_changes and comment
	CommitID string      `json:"commitId,omitzero"` // empty: the current head
}
