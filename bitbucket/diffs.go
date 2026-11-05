package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

var (
	_ gitoperationslib.CommitDiffs    = (*Client)(nil)
	_ gitoperationslib.PRContents     = (*Client)(nil)
	_ gitoperationslib.ReviewComments = (*Client)(nil)
)

// CommitDiff implements gitoperationslib.CommitDiffs. It first resolves ref:
// a commit's diff is addressed by its hash.
func (c *Client) CommitDiff(ctx context.Context, owner, repo, ref string, format gitoperationslib.DiffFormat) (io.ReadCloser, error) {
	if ref == "" {
		return nil, errors.New("bitbucket: commits.diff: ref is required")
	}
	if !format.Valid() {
		return nil, fmt.Errorf("bitbucket: commits.diff: invalid format %q", format)
	}
	j, err := commits{c}.resolve(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	return c.api.Stream(ctx, repoPath(owner, repo, string(format), j.Hash))
}

// PullRequestDiff implements gitoperationslib.PRContents.
func (c *Client) PullRequestDiff(ctx context.Context, owner, repo string, number int, format gitoperationslib.DiffFormat) (io.ReadCloser, error) {
	if !format.Valid() {
		return nil, fmt.Errorf("bitbucket: pulls.diff: invalid format %q", format)
	}
	return c.api.Stream(ctx, pullsPath(owner, repo, strconv.Itoa(number), string(format)))
}

// PullRequestFiles implements gitoperationslib.PRContents.
func (c *Client) PullRequestFiles(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.ChangedFile], error) {
	values, next, err := bitbucketapi.ListPaged[diffstatJSON](ctx, c.api, pullsPath(owner, repo, strconv.Itoa(number), "diffstat"), bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.ChangedFile]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.ChangedFile]{Items: make([]gitoperationslib.ChangedFile, len(values)), NextPage: next}
	for i := range values {
		out.Items[i] = values[i].toChangedFile()
	}
	return out, nil
}

// PullRequestCommits implements gitoperationslib.PRContents. This endpoint has
// page tokens, which Page.NextPage cannot carry (page=2 is refused with HTTP
// 400, checked 2026-11-05), so the commits all come at once.
func (c *Client) PullRequestCommits(ctx context.Context, owner, repo string, number int, _ gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
	all, err := bitbucketapi.All[commitJSON](ctx, c.api, pullsPath(owner, repo, strconv.Itoa(number), "commits"), nil)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Commit]{Items: make([]gitoperationslib.Commit, len(all))}
	for i := range all {
		out.Items[i] = all[i].toCommit()
	}
	return out, nil
}

type prCommentJSON struct {
	ID      int64 `json:"id"`
	Content struct {
		Raw string `json:"raw"`
	} `json:"content"`
	User    *userJSON `json:"user"`
	Deleted bool      `json:"deleted"`
	Inline  *struct {
		Path string `json:"path"`
		From *int   `json:"from"`
		To   *int   `json:"to"`
	} `json:"inline"`
	CreatedOn time.Time `json:"created_on"`
	UpdatedOn time.Time `json:"updated_on"`
	Links     struct {
		HTML linkJSON `json:"html"`
	} `json:"links"`
}

func (j *prCommentJSON) toReviewComment() gitoperationslib.ReviewComment {
	rc := gitoperationslib.ReviewComment{
		ID: j.ID, Author: j.User.login(), Body: j.Content.Raw, HTMLURL: j.Links.HTML.Href,
		CreatedAt: j.CreatedOn, UpdatedAt: j.UpdatedOn,
	}
	if j.Inline != nil {
		rc.Path = j.Inline.Path
		if j.Inline.To != nil {
			rc.Line = *j.Inline.To
		}
		if j.Inline.From != nil {
			rc.OldLine = *j.Inline.From
		}
	}
	return rc
}

// ListReviewComments implements gitoperationslib.ReviewComments. Bitbucket
// lists line comments with the others, and keeps deleted ones, so they are
// picked out of the full list.
func (c *Client) ListReviewComments(ctx context.Context, owner, repo string, number int, _ gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.ReviewComment], error) {
	all, err := bitbucketapi.All[prCommentJSON](ctx, c.api, pullsPath(owner, repo, strconv.Itoa(number), "comments"), nil)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.ReviewComment]{}, err
	}
	page := gitoperationslib.Page[gitoperationslib.ReviewComment]{Items: []gitoperationslib.ReviewComment{}}
	for i := range all {
		if all[i].Inline != nil && !all[i].Deleted {
			page.Items = append(page.Items, all[i].toReviewComment())
		}
	}
	return page, nil
}

// CreateReviewComment implements gitoperationslib.ReviewComments. Comments
// are tied to the pull request, not to a commit: CommitID is not supported.
func (c *Client) CreateReviewComment(ctx context.Context, owner, repo string, number int, opts gitoperationslib.CreateReviewCommentOptions) (*gitoperationslib.ReviewComment, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("bitbucket: pulls.create_review_comment: %w", err)
	}
	if opts.CommitID != "" {
		return nil, notSupported("pulls.create_review_comment", "commenting on a specific commit")
	}
	inline := map[string]any{"path": opts.Path}
	if opts.Line > 0 {
		inline["to"] = opts.Line
	} else {
		inline["from"] = opts.OldLine
	}
	body := map[string]any{"content": map[string]string{"raw": opts.Body}, "inline": inline}
	var out prCommentJSON
	if _, err := c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, strconv.Itoa(number), "comments"), nil, body, &out); err != nil {
		return nil, err
	}
	rc := out.toReviewComment()
	return &rc, nil
}

// DeleteReviewComment implements gitoperationslib.ReviewComments.
func (c *Client) DeleteReviewComment(ctx context.Context, owner, repo string, number int, id int64) error {
	_, err := c.api.Do(ctx, http.MethodDelete, pullsPath(owner, repo, strconv.Itoa(number), "comments", strconv.FormatInt(id, 10)), nil, nil, nil)
	return err
}
