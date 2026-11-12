package forgejo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

// Diff implements gitoperationslib.Commits.
func (m commits) Diff(ctx context.Context, owner, repo, ref string, format gitoperationslib.DiffFormat) (io.ReadCloser, error) {
	c := m.c
	if ref == "" {
		return nil, errors.New("forgejo: commits.diff: ref is required")
	}
	if !format.Valid() {
		return nil, fmt.Errorf("forgejo: commits.diff: invalid format %q", format)
	}
	return c.api.Stream(ctx, repoPath(owner, repo, "git", "commits", ref+"."+string(format)))
}

// Diff implements gitoperationslib.PullRequests.
func (p pulls) Diff(ctx context.Context, owner, repo string, number int, format gitoperationslib.DiffFormat) (io.ReadCloser, error) {
	c := p.c
	if !format.Valid() {
		return nil, fmt.Errorf("forgejo: pulls.diff: invalid format %q", format)
	}
	return c.api.Stream(ctx, pullsPath(owner, repo, strconv.Itoa(number)+"."+string(format)))
}

type prFileJSON struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
}

func (f *prFileJSON) toChangedFile() gitoperationslib.ChangedFile {
	status := gitoperationslib.FileStatus(f.Status)
	// A pull request's modified files are "changed", a commit's "modified".
	if f.Status == "changed" {
		status = gitoperationslib.FileModified
	}
	return gitoperationslib.ChangedFile{
		Path: f.Filename, PreviousPath: f.PreviousFilename, Status: status,
		Additions: f.Additions, Deletions: f.Deletions,
	}
}

// Files implements gitoperationslib.PullRequests.
func (p pulls) Files(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.ChangedFile], error) {
	c := p.c
	return listAt(c, pullsPath(owner, repo, strconv.Itoa(number), "files"), (*prFileJSON).toChangedFile)(ctx, opts)
}

// Commits implements gitoperationslib.PullRequests.
func (p pulls) Commits(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
	c := p.c
	q := forgejoapi.ListQuery(opts)
	q.Set("files", "false")
	q.Set("verification", "false")
	var out []commitJSON
	resp, err := c.api.Do(ctx, http.MethodGet, pullsPath(owner, repo, strconv.Itoa(number), "commits"), q, nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Commit]{}, err
	}
	return toPage(resp, out, func(j *commitJSON) gitoperationslib.Commit { return j.toCommit(false) }), nil
}

type reviewCommentJSON struct {
	ID       int64  `json:"id"`
	Body     string `json:"body"`
	Path     string `json:"path"`
	Position int    `json:"position"`
	Original int    `json:"original_position"`
	Extra    int    `json:"extra_lines_count"`
	CommitID string `json:"commit_id"`
	ReviewID int64  `json:"pull_request_review_id"`
	HTMLURL  string `json:"html_url"`
	User     *struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (j *reviewCommentJSON) toReviewComment() gitoperationslib.ReviewComment {
	rc := gitoperationslib.ReviewComment{
		ID: j.ID, Body: j.Body, Path: j.Path, Line: j.Position, OldLine: j.Original, ExtraLines: j.Extra, CommitID: j.CommitID,
		ReviewID: j.ReviewID, HTMLURL: j.HTMLURL, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	if j.User != nil {
		rc.Author = j.User.Login
	}
	return rc
}

func (c *Client) reviewComments(ctx context.Context, owner, repo string, number int, reviewID int64) ([]reviewCommentJSON, error) {
	var out []reviewCommentJSON
	path := pullsPath(owner, repo, strconv.Itoa(number), "reviews", strconv.FormatInt(reviewID, 10), "comments")
	if _, err := c.api.Do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListReviewComments implements gitoperationslib.PullRequests. Forgejo only
// lists comments review by review, so they all come at once.
func (p pulls) ListReviewComments(ctx context.Context, owner, repo string, number int, _ gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.ReviewComment], error) {
	c := p.c
	type review struct {
		ID       int64 `json:"id"`
		Comments int   `json:"comments_count"`
	}
	list := listAt(c, pullsPath(owner, repo, strconv.Itoa(number), "reviews"), func(r *review) review { return *r })
	page := gitoperationslib.Page[gitoperationslib.ReviewComment]{Items: []gitoperationslib.ReviewComment{}}
	for r, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{}, list) {
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.ReviewComment]{}, err
		}
		if r.Comments == 0 {
			continue
		}
		comments, err := c.reviewComments(ctx, owner, repo, number, r.ID)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.ReviewComment]{}, err
		}
		for i := range comments {
			page.Items = append(page.Items, comments[i].toReviewComment())
		}
	}
	slices.SortFunc(page.Items, func(a, b gitoperationslib.ReviewComment) int { return int(a.ID - b.ID) })
	return page, nil
}

// multiLineCommentsVersion is the first release with extra_lines_count. Older
// ones ignore it.
var multiLineCommentsVersion = Version{16, 0, 0}

// CreateReviewComment implements gitoperationslib.PullRequests: comments
// only exist within a review, so it creates one.
func (p pulls) CreateReviewComment(ctx context.Context, owner, repo string, number int, opts gitoperationslib.CreateReviewCommentOptions) (*gitoperationslib.ReviewComment, error) {
	c := p.c
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("forgejo: pulls.create_review_comment: %w", err)
	}
	if opts.ExtraLines > 0 && c.version.Compare(multiLineCommentsVersion) < 0 {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "pulls.create_review_comment", "comments on several lines before Forgejo "+multiLineCommentsVersion.String())
	}
	type comment struct {
		Path        string `json:"path"`
		Body        string `json:"body"`
		NewPosition int    `json:"new_position,omitempty"`
		OldPosition int    `json:"old_position,omitempty"`
		ExtraLines  int    `json:"extra_lines_count,omitempty"`
	}
	body := struct {
		Event    string    `json:"event"`
		CommitID string    `json:"commit_id,omitempty"`
		Comments []comment `json:"comments"`
	}{"COMMENT", opts.CommitID, []comment{{opts.Path, opts.Body, opts.Line, opts.OldLine, opts.ExtraLines}}}
	var review struct {
		ID int64 `json:"id"`
	}
	if _, err := c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, strconv.Itoa(number), "reviews"), nil, body, &review); err != nil {
		return nil, err
	}
	comments, err := c.reviewComments(ctx, owner, repo, number, review.ID)
	if err != nil {
		return nil, err
	}
	if len(comments) != 1 {
		return nil, fmt.Errorf("forgejo: pulls.create_review_comment: review %d holds %d comments, want 1", review.ID, len(comments))
	}
	rc := comments[0].toReviewComment()
	return &rc, nil
}

// DeleteReviewComment implements gitoperationslib.PullRequests. The path
// needs the comment's review, so it lists the comments first. DELETE
// /issues/comments/{id} is no shortcut: it answers 204 for a line comment and
// leaves it in place (checked on 15.0.11).
func (p pulls) DeleteReviewComment(ctx context.Context, owner, repo string, number int, id int64) error {
	c := p.c
	page, err := pulls{c}.ListReviewComments(ctx, owner, repo, number, gitoperationslib.ListOptions{})
	if err != nil {
		return err
	}
	i := slices.IndexFunc(page.Items, func(rc gitoperationslib.ReviewComment) bool { return rc.ID == id })
	if i < 0 {
		return &gitoperationslib.APIError{
			Backend: gitoperationslib.BackendForgejo, StatusCode: http.StatusNotFound,
			Message: fmt.Sprintf("line comment %d not found on pull request %d of %s/%s", id, number, owner, repo),
		}
	}
	path := pullsPath(owner, repo, strconv.Itoa(number), "reviews", strconv.FormatInt(page.Items[i].ReviewID, 10), "comments", strconv.FormatInt(id, 10))
	_, err = c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}
