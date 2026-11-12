package gitoperationslib

import (
	"context"
	"errors"
	"io"
	"time"
)

// PullRequests manages pull requests, their reviews and line comments. Diff,
// Files and Commits need CapPRContents; the review comment methods
// CapReviewComments.
type PullRequests interface {
	List(ctx context.Context, owner, repo string, opts ListPROptions) (Page[PullRequest], error)
	Get(ctx context.Context, owner, repo string, number int) (*PullRequest, error)
	// Create opens a pull request. If the server does not honour Draft
	// (Forgejo with custom work-in-progress prefixes), the pull request is
	// returned together with an error matching ErrNotSupported.
	Create(ctx context.Context, owner, repo string, opts CreatePROptions) (*PullRequest, error)
	// Edit changes a pull request; see EditPROptions. A Draft change the
	// server does not honour is reported as in Create.
	Edit(ctx context.Context, owner, repo string, number int, opts EditPROptions) (*PullRequest, error)
	// Merge merges a pull request. Drafts cannot be merged. With
	// MergeOptions.WhenChecksSucceed the merge is scheduled instead.
	Merge(ctx context.Context, owner, repo string, number int, opts MergeOptions) error
	// CancelAutoMerge cancels a merge scheduled with WhenChecksSucceed.
	CancelAutoMerge(ctx context.Context, owner, repo string, number int) error
	IsMerged(ctx context.Context, owner, repo string, number int) (bool, error)

	RequestReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error
	RemoveReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error
	ListReviews(ctx context.Context, owner, repo string, number int, opts ListOptions) (Page[Review], error)
	CreateReview(ctx context.Context, owner, repo string, number int, opts CreateReviewOptions) (*Review, error)

	// Diff streams the pull request's changes; the caller must
	// close it. GitHub refuses diffs that are too large (HTTP 406).
	Diff(ctx context.Context, owner, repo string, number int, format DiffFormat) (io.ReadCloser, error)
	// Files lists the changed files. GitHub lists at most 3000.
	Files(ctx context.Context, owner, repo string, number int, opts ListOptions) (Page[ChangedFile], error)
	// Commits lists the commits, newest first, without Files or
	// Stats. GitHub lists at most 250: the oldest ones of a longer pull
	// request, and it takes one more request per page to put them in order.
	// Bitbucket returns them all at once, ignoring opts.
	Commits(ctx context.Context, owner, repo string, number int, opts ListOptions) (Page[Commit], error)

	// ListReviewComments lists the line comments. Forgejo (one request per
	// review with comments) and Bitbucket return them all at once, ignoring
	// opts.
	ListReviewComments(ctx context.Context, owner, repo string, number int, opts ListOptions) (Page[ReviewComment], error)
	// CreateReviewComment comments on one line. On Forgejo it creates a
	// review holding the comment, and the server checks neither the path nor
	// the line. GitHub refuses a line outside the diff.
	CreateReviewComment(ctx context.Context, owner, repo string, number int, opts CreateReviewCommentOptions) (*ReviewComment, error)
	// DeleteReviewComment deletes a line comment. On Forgejo, the review
	// that held it stays, empty.
	DeleteReviewComment(ctx context.Context, owner, repo string, number int, id int64) error
}

// Valid reports whether f is one of the formats above.
func (f DiffFormat) Valid() bool { return f == DiffUnified || f == DiffPatch }

// ReviewComment is a comment on a line of a pull request's diff.
type ReviewComment struct {
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
	Path   string `json:"path"`
	// Line is the line in the new version of the file, OldLine in the old
	// one. Only one is usually set. Bitbucket may report both.
	Line    int `json:"line,omitzero"`
	OldLine int `json:"oldLine,omitzero"`
	// ExtraLines is how many lines after that one the comment also covers.
	ExtraLines int `json:"extraLines,omitzero"`
	// CommitID is the commit commented on; Bitbucket does not report it.
	CommitID string `json:"commitId,omitzero"`
	// ReviewID is the review holding the comment; Bitbucket has none.
	ReviewID  int64     `json:"reviewId,omitzero"`
	HTMLURL   string    `json:"htmlUrl"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// CreateReviewCommentOptions describes a line comment. Path, Body and exactly
// one of Line and OldLine are required.
type CreateReviewCommentOptions struct {
	Path string `json:"path"`
	Body string `json:"body"`
	// Line is a line of the new version of the file, OldLine one of the old
	// version (a removed line).
	Line    int `json:"line,omitzero"`
	OldLine int `json:"oldLine,omitzero"`
	// ExtraLines extends the comment to that many lines after Line or OldLine.
	// Forgejo needs version 16 (ErrNotSupported on older servers, which would
	// ignore it), and takes at most 50.
	ExtraLines int `json:"extraLines,omitzero"`
	// CommitID is the commit to comment on; empty means the pull request's
	// head. Not supported on Bitbucket.
	CommitID string `json:"commitId,omitzero"`
}

// Validate reports a missing or contradictory field.
func (o CreateReviewCommentOptions) Validate() error {
	switch {
	case o.Path == "":
		return errors.New("path is required")
	case o.Body == "":
		return errors.New("body is required")
	case o.ExtraLines < 0:
		return errors.New("extra lines must not be negative")
	case (o.Line > 0) == (o.OldLine > 0):
		return errors.New("exactly one of Line and OldLine is required")
	}
	return nil
}

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
	// Base and Head select pull requests by the branch they merge into, and the
	// branch they merge from. Forgejo needs version 16 (ErrNotSupported on older
	// servers, which would ignore them and return everything). A plain branch
	// name is looked up in the repository's owner on GitHub; "user:branch", for a
	// fork, works on GitHub only.
	Base string `json:"base,omitzero"`
	Head string `json:"head,omitzero"`
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
