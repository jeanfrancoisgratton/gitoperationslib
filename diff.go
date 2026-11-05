package gitoperationslib

import (
	"context"
	"errors"
	"io"
	"time"
)

// DiffFormat selects the format of a raw diff.
type DiffFormat string

const (
	// DiffUnified is a plain unified diff, as git diff prints it.
	DiffUnified DiffFormat = "diff"
	// DiffPatch is the mail-formatted patch git format-patch prints, with
	// each commit's author and message, ready for git am.
	DiffPatch DiffFormat = "patch"
)

// Valid reports whether f is one of the formats above.
func (f DiffFormat) Valid() bool { return f == DiffUnified || f == DiffPatch }

// CommitDiffs reads a commit's raw diff. The Forgejo, GitHub and Bitbucket
// clients implement it (CapCommitDiffs); find it with a type assertion:
//
//	d, ok := client.(gitoperationslib.CommitDiffs)
type CommitDiffs interface {
	// CommitDiff streams the changes of a commit against its first parent;
	// the caller must close it. ref is a commit SHA, a branch or a tag.
	CommitDiff(ctx context.Context, owner, repo, ref string, format DiffFormat) (io.ReadCloser, error)
}

// PRContents reads what a pull request changes. The Forgejo, GitHub and
// Bitbucket clients implement it (CapPRContents); find it with a type
// assertion:
//
//	c, ok := client.(gitoperationslib.PRContents)
type PRContents interface {
	// PullRequestDiff streams the pull request's changes; the caller must
	// close it. GitHub refuses diffs that are too large (HTTP 406).
	PullRequestDiff(ctx context.Context, owner, repo string, number int, format DiffFormat) (io.ReadCloser, error)
	// PullRequestFiles lists the changed files. GitHub lists at most 3000.
	PullRequestFiles(ctx context.Context, owner, repo string, number int, opts ListOptions) (Page[ChangedFile], error)
	// PullRequestCommits lists the commits, newest first, without Files or
	// Stats. GitHub lists at most 250: the oldest ones of a longer pull
	// request, and it takes one more request per page to put them in order.
	// Bitbucket returns them all at once, ignoring opts.
	PullRequestCommits(ctx context.Context, owner, repo string, number int, opts ListOptions) (Page[Commit], error)
}

// ReviewComments manages the comments on a pull request's lines. The Forgejo,
// GitHub and Bitbucket clients implement it (CapReviewComments); find it with
// a type assertion:
//
//	rc, ok := client.(gitoperationslib.ReviewComments)
type ReviewComments interface {
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
	case (o.Line > 0) == (o.OldLine > 0):
		return errors.New("exactly one of Line and OldLine is required")
	}
	return nil
}
