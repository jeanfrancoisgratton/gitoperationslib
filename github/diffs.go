package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var (
	_ gitoperationslib.CommitDiffs    = (*Client)(nil)
	_ gitoperationslib.PRContents     = (*Client)(nil)
	_ gitoperationslib.ReviewComments = (*Client)(nil)
)

// maxPRCommits is how many commits of a pull request GitHub lists.
const maxPRCommits = 250

func rawOptions(op string, format gitoperationslib.DiffFormat) (gh.RawOptions, error) {
	switch format {
	case gitoperationslib.DiffUnified:
		return gh.RawOptions{Type: gh.Diff}, nil
	case gitoperationslib.DiffPatch:
		return gh.RawOptions{Type: gh.Patch}, nil
	}
	return gh.RawOptions{}, fmt.Errorf("github: %s: invalid format %q", op, format)
}

// CommitDiff implements gitoperationslib.CommitDiffs.
func (c *Client) CommitDiff(ctx context.Context, owner, repo, ref string, format gitoperationslib.DiffFormat) (io.ReadCloser, error) {
	if ref == "" {
		return nil, errors.New("github: commits.diff: ref is required")
	}
	raw, err := rawOptions("commits.diff", format)
	if err != nil {
		return nil, err
	}
	diff, _, err := c.gh.Repositories.GetCommitRaw(ctx, owner, repo, ref, raw)
	if err != nil {
		return nil, commitError(err)
	}
	return io.NopCloser(strings.NewReader(diff)), nil
}

// commitError converts the error of reading a commit. GitHub answers HTTP 422
// for an unknown SHA (checked 2026-11-05), the other forges 404.
func commitError(err error) error {
	err = convertError(err)
	var apiErr *gitoperationslib.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity && strings.Contains(apiErr.Message, "No commit found") {
		apiErr.StatusCode = http.StatusNotFound
	}
	return err
}

// PullRequestDiff implements gitoperationslib.PRContents.
func (c *Client) PullRequestDiff(ctx context.Context, owner, repo string, number int, format gitoperationslib.DiffFormat) (io.ReadCloser, error) {
	raw, err := rawOptions("pulls.diff", format)
	if err != nil {
		return nil, err
	}
	diff, _, err := c.gh.PullRequests.GetRaw(ctx, owner, repo, number, raw)
	if err != nil {
		return nil, convertError(err)
	}
	return io.NopCloser(strings.NewReader(diff)), nil
}

// PullRequestFiles implements gitoperationslib.PRContents.
func (c *Client) PullRequestFiles(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.ChangedFile], error) {
	lo := ghList(opts)
	files, resp, err := c.gh.PullRequests.ListFiles(ctx, owner, repo, number, &lo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.ChangedFile]{}, convertError(err)
	}
	return toPage(files, resp, toChangedFile), nil
}

// PullRequestCommits implements gitoperationslib.PRContents.
func (c *Client) PullRequestCommits(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, convertError(err)
	}
	return newestFirst(min(pr.GetCommits(), maxPRCommits), opts, func(lo gh.ListOptions) ([]*gh.RepositoryCommit, error) {
		list, _, err := c.gh.PullRequests.ListCommits(ctx, owner, repo, number, &lo)
		return list, convertError(err)
	})
}

// newestFirst returns a page of total commits, newest first, from a list the
// server only gives oldest first. A page taken from the end of that list
// usually straddles two of the server's pages.
func newestFirst(total int, opts gitoperationslib.ListOptions, fetch func(gh.ListOptions) ([]*gh.RepositoryCommit, error)) (gitoperationslib.Page[gitoperationslib.Commit], error) {
	size := opts.PerPage
	if size <= 0 {
		size = 30
	}
	size = min(size, 100)
	page := max(opts.Page, 1)
	out := gitoperationslib.Page[gitoperationslib.Commit]{Items: []gitoperationslib.Commit{}}
	// [lo, hi) are the positions of this page in the oldest-first list.
	hi := total - (page-1)*size
	if hi <= 0 {
		return out, nil
	}
	lo := max(hi-size, 0)
	first, last := lo/size+1, (hi-1)/size+1
	var oldestFirst []*gh.RepositoryCommit
	for p := first; p <= last; p++ {
		list, err := fetch(gh.ListOptions{Page: p, PerPage: size})
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Commit]{}, err
		}
		oldestFirst = append(oldestFirst, list...)
	}
	base := (first - 1) * size
	window := oldestFirst[min(lo-base, len(oldestFirst)):min(hi-base, len(oldestFirst))]
	for _, rc := range slices.Backward(window) {
		out.Items = append(out.Items, toCommit(rc))
	}
	if lo > 0 {
		out.NextPage = page + 1
	}
	return out, nil
}

func toReviewComment(c *gh.PullRequestComment) gitoperationslib.ReviewComment {
	rc := gitoperationslib.ReviewComment{
		ID: c.GetID(), Author: c.GetUser().GetLogin(), Body: c.GetBody(), Path: c.GetPath(),
		CommitID: c.GetCommitID(), ReviewID: c.GetPullRequestReviewID(), HTMLURL: c.GetHTMLURL(),
		CreatedAt: c.GetCreatedAt().Time, UpdatedAt: c.GetUpdatedAt().Time,
	}
	// An outdated comment has no current line, only the original one.
	line := c.GetLine()
	if line == 0 {
		line = c.GetOriginalLine()
	}
	// A comment on several lines reports its last one here, and the first apart.
	start := c.GetStartLine()
	if start == 0 {
		start = c.GetOriginalStartLine()
	}
	if start > 0 && start < line {
		line, rc.ExtraLines = start, line-start
	}
	if c.GetSide() == sideOld {
		rc.OldLine = line
	} else {
		rc.Line = line
	}
	return rc
}

const (
	sideNew = "RIGHT"
	sideOld = "LEFT"
)

// ListReviewComments implements gitoperationslib.ReviewComments.
func (c *Client) ListReviewComments(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.ReviewComment], error) {
	comments, resp, err := c.gh.PullRequests.ListComments(ctx, owner, repo, number, &gh.PullRequestListCommentsOptions{ListOptions: ghList(opts)})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.ReviewComment]{}, convertError(err)
	}
	return toPage(comments, resp, toReviewComment), nil
}

// CreateReviewComment implements gitoperationslib.ReviewComments. GitHub
// requires a commit, so an empty CommitID costs one request for the head.
func (c *Client) CreateReviewComment(ctx context.Context, owner, repo string, number int, opts gitoperationslib.CreateReviewCommentOptions) (*gitoperationslib.ReviewComment, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("github: pulls.create_review_comment: %w", err)
	}
	commit := opts.CommitID
	if commit == "" {
		pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
		if err != nil {
			return nil, convertError(err)
		}
		commit = pr.GetHead().GetSHA()
	}
	line, side := opts.Line, sideNew
	if opts.OldLine > 0 {
		line, side = opts.OldLine, sideOld
	}
	request := gh.CreatePullRequestCommentRequest{
		Path: opts.Path, Body: opts.Body, CommitID: commit, Line: gh.Ptr(line + opts.ExtraLines), Side: gh.Ptr(side),
	}
	if opts.ExtraLines > 0 {
		request.StartLine, request.StartSide = gh.Ptr(line), gh.Ptr(side)
	}
	created, _, err := c.gh.PullRequests.CreateComment(ctx, owner, repo, number, request)
	if err != nil {
		return nil, convertError(err)
	}
	rc := toReviewComment(created)
	return &rc, nil
}

// DeleteReviewComment implements gitoperationslib.ReviewComments.
func (c *Client) DeleteReviewComment(ctx context.Context, owner, repo string, _ int, id int64) error {
	_, err := c.gh.PullRequests.DeleteComment(ctx, owner, repo, id)
	return convertError(err)
}
