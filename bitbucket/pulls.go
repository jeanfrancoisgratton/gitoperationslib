package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

type endpointJSON struct {
	Branch struct {
		Name string `json:"name"`
	} `json:"branch"`
	Commit *struct {
		Hash string `json:"hash"`
	} `json:"commit"`
	Repository *struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (e *endpointJSON) toBranch() gitoperationslib.PRBranch {
	b := gitoperationslib.PRBranch{Ref: e.Branch.Name}
	if e.Commit != nil {
		b.SHA = e.Commit.Hash // Bitbucket reports 12-character hashes
	}
	if e.Repository != nil {
		b.Repo = e.Repository.FullName
	}
	return b
}

type participantJSON struct {
	User           userJSON   `json:"user"`
	Role           string     `json:"role"`
	Approved       bool       `json:"approved"`
	State          *string    `json:"state"` // "approved", "changes_requested" or null
	ParticipatedOn *time.Time `json:"participated_on"`
}

type pullJSON struct {
	ID           int                    `json:"id"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description"`
	State        string                 `json:"state"` // OPEN, MERGED, DECLINED, SUPERSEDED
	Draft        bool                   `json:"draft"`
	Author       *userJSON              `json:"author"`
	Source       endpointJSON           `json:"source"`
	Destination  endpointJSON           `json:"destination"`
	MergeCommit  *struct{ Hash string } `json:"merge_commit"`
	ClosedBy     *userJSON              `json:"closed_by"`
	Reviewers    []userJSON             `json:"reviewers"`
	Participants []participantJSON      `json:"participants"`
	Links        struct {
		HTML linkJSON `json:"html"`
	} `json:"links"`
	CreatedOn time.Time `json:"created_on"`
	UpdatedOn time.Time `json:"updated_on"`
}

func (p *pullJSON) toPR() gitoperationslib.PullRequest {
	pr := gitoperationslib.PullRequest{
		Number: p.ID, Title: p.Title, Body: p.Description, State: gitoperationslib.PRStateOpen,
		Draft: p.Draft, Author: p.Author.login(), Head: p.Source.toBranch(), Base: p.Destination.toBranch(),
		Merged: p.State == "MERGED", HTMLURL: p.Links.HTML.Href, CreatedAt: p.CreatedOn, UpdatedAt: p.UpdatedOn,
	}
	if p.State != "OPEN" {
		pr.State = gitoperationslib.PRStateClosed
		pr.ClosedAt = p.UpdatedOn
	}
	if pr.Merged {
		pr.MergedBy = p.ClosedBy.login()
		pr.MergedAt = p.UpdatedOn
		if p.MergeCommit != nil {
			pr.MergeCommitSHA = p.MergeCommit.Hash
		}
	}
	return pr
}

func pullsPath(owner, repo string, rest ...string) string {
	return repoPath(owner, repo, append([]string{"pullrequests"}, rest...)...)
}

// PullRequests implements gitoperationslib.Client.
//
// Declined (closed) pull requests cannot be reopened, and Bitbucket allows
// authors to approve their own pull requests.
func (c *Client) PullRequests() gitoperationslib.PullRequests { return pulls{c} }

type pulls struct{ c *Client }

func (p pulls) get(ctx context.Context, owner, repo string, number int) (*pullJSON, error) {
	var out pullJSON
	if _, err := p.c.api.Do(ctx, http.MethodGet, pullsPath(owner, repo, strconv.Itoa(number)), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p pulls) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListPROptions) (gitoperationslib.Page[gitoperationslib.PullRequest], error) {
	q := bitbucketapi.ListQuery(opts.ListOptions)
	switch opts.State {
	case "", gitoperationslib.PRStateOpen:
		q["state"] = []string{"OPEN"}
	case gitoperationslib.PRStateClosed:
		q["state"] = []string{"MERGED", "DECLINED", "SUPERSEDED"}
	case gitoperationslib.PRStateAll:
		q["state"] = []string{"OPEN", "MERGED", "DECLINED", "SUPERSEDED"}
	default:
		return gitoperationslib.Page[gitoperationslib.PullRequest]{}, fmt.Errorf("bitbucket: pulls.list: invalid state %q", opts.State)
	}
	page, err := bitbucketapi.List[pullJSON](ctx, p.c.api, pullsPath(owner, repo), q)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.PullRequest]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.PullRequest]{Items: make([]gitoperationslib.PullRequest, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = page.Values[i].toPR()
	}
	return out, nil
}

func (p pulls) Get(ctx context.Context, owner, repo string, number int) (*gitoperationslib.PullRequest, error) {
	out, err := p.get(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	pr := out.toPR()
	return &pr, nil
}

func branchRef(name string) map[string]any {
	return map[string]any{"branch": map[string]string{"name": name}}
}

func (p pulls) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreatePROptions) (*gitoperationslib.PullRequest, error) {
	const op = "pulls.create"
	if opts.Title == "" || opts.Head == "" || opts.Base == "" {
		return nil, errors.New("bitbucket: pulls.create: title, head and base are required")
	}
	if strings.Contains(opts.Head, ":") {
		return nil, notSupported(op, "pull requests from forks")
	}
	if opts.AllowMaintainerEdit != nil {
		return nil, notSupported(op, "maintainer edits")
	}
	body := map[string]any{
		"title": opts.Title, "description": opts.Body, "draft": opts.Draft,
		"source": branchRef(opts.Head), "destination": branchRef(opts.Base),
	}
	var out pullJSON
	if _, err := p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo), nil, body, &out); err != nil {
		return nil, err
	}
	pr := out.toPR()
	return &pr, nil
}

// Edit changes a pull request. Closing declines it; a declined pull request
// cannot be reopened.
func (p pulls) Edit(ctx context.Context, owner, repo string, number int, opts gitoperationslib.EditPROptions) (*gitoperationslib.PullRequest, error) {
	const op = "pulls.edit"
	if opts.AllowMaintainerEdit != nil {
		return nil, notSupported(op, "maintainer edits")
	}
	body := map[string]any{}
	if opts.Title != nil {
		body["title"] = *opts.Title
	}
	if opts.Body != nil {
		body["description"] = *opts.Body
	}
	if opts.Base != nil {
		body["destination"] = branchRef(*opts.Base)
	}
	if opts.Draft != nil {
		body["draft"] = *opts.Draft
	}
	n := strconv.Itoa(number)
	if opts.State != nil {
		switch *opts.State {
		case gitoperationslib.PRStateOpen:
			cur, err := p.get(ctx, owner, repo, number)
			if err != nil {
				return nil, err
			}
			if cur.State != "OPEN" {
				return nil, notSupported(op, "reopening a "+strings.ToLower(cur.State)+" pull request")
			}
		case gitoperationslib.PRStateClosed:
		default:
			return nil, fmt.Errorf("bitbucket: %s: invalid state %q", op, *opts.State)
		}
	}
	if len(body) > 0 {
		if _, err := p.c.api.Do(ctx, http.MethodPut, pullsPath(owner, repo, n), nil, body, nil); err != nil {
			return nil, err
		}
	}
	if opts.State != nil && *opts.State == gitoperationslib.PRStateClosed {
		if _, err := p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, n, "decline"), nil, nil, nil); err != nil {
			return nil, err
		}
	}
	return p.Get(ctx, owner, repo, number)
}

var mergeStrategies = map[gitoperationslib.MergeMethod]string{
	gitoperationslib.MergeMethodMerge:           "merge_commit",
	gitoperationslib.MergeMethodSquash:          "squash",
	gitoperationslib.MergeMethodFastForwardOnly: "fast_forward",
	gitoperationslib.MergeMethodRebase:          "rebase_fast_forward",
	gitoperationslib.MergeMethodRebaseMerge:     "rebase_merge",
}

// Merge merges a pull request. Bitbucket cannot make the merge conditional on
// the head commit: HeadSHA is checked just before merging.
func (p pulls) Merge(ctx context.Context, owner, repo string, number int, opts gitoperationslib.MergeOptions) error {
	const op = "pulls.merge"
	method := opts.Method
	if method == "" {
		method = gitoperationslib.MergeMethodMerge
	}
	strategy, ok := mergeStrategies[method]
	if !ok {
		if !method.Valid() {
			return fmt.Errorf("bitbucket: %s: invalid merge method %q", op, method)
		}
		return notSupported(op, fmt.Sprintf("merge method %q", method))
	}
	if opts.WhenChecksSucceed {
		return notSupported(op, "auto-merge")
	}
	if opts.HeadSHA != "" {
		cur, err := p.get(ctx, owner, repo, number)
		if err != nil {
			return err
		}
		head := cur.Source.toBranch().SHA
		if head == "" || !(strings.HasPrefix(opts.HeadSHA, head) || strings.HasPrefix(head, opts.HeadSHA)) {
			return &gitoperationslib.APIError{
				Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusConflict,
				Message: fmt.Sprintf("head of #%d is %s, not %s", number, head, opts.HeadSHA),
			}
		}
	}
	message := strings.TrimSpace(opts.Title + "\n\n" + opts.Message)
	body := map[string]any{"merge_strategy": strategy, "close_source_branch": opts.DeleteBranch}
	if message != "" {
		body["message"] = message
	}
	resp, err := p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, strconv.Itoa(number), "merge"), nil, body, nil)
	if err != nil {
		return err
	}
	// Long merges run asynchronously (202): wait for the result.
	if resp.StatusCode == http.StatusAccepted {
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
			if merged, err := p.IsMerged(ctx, owner, repo, number); err != nil || merged {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		return fmt.Errorf("bitbucket: %s: #%d still not merged after a minute", op, number)
	}
	return nil
}

func (p pulls) IsMerged(ctx context.Context, owner, repo string, number int) (bool, error) {
	cur, err := p.get(ctx, owner, repo, number)
	if gitoperationslib.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return cur.State == "MERGED", nil
}

func (p pulls) CancelAutoMerge(context.Context, string, string, int) error {
	return notSupported("pulls.cancel_auto_merge", "auto-merge")
}

// setReviewers rewrites the reviewer list with add/remove applied.
func (p pulls) setReviewers(ctx context.Context, owner, repo string, number int, users, teams []string, add bool) error {
	op := "pulls.request_reviewers"
	if !add {
		op = "pulls.remove_reviewers"
	}
	if len(teams) > 0 {
		return notSupported(op, "team reviewers")
	}
	cur, err := p.get(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	uuids := map[string]bool{}
	for _, r := range cur.Reviewers {
		uuids[r.UUID] = true
	}
	for _, who := range users {
		uuid, err := p.c.resolveUser(ctx, owner, who)
		if err != nil {
			return err
		}
		uuids[uuid] = add
	}
	reviewers := []map[string]string{}
	for uuid, keep := range uuids {
		if keep {
			reviewers = append(reviewers, map[string]string{"uuid": uuid})
		}
	}
	_, err = p.c.api.Do(ctx, http.MethodPut, pullsPath(owner, repo, strconv.Itoa(number)), nil,
		map[string]any{"title": cur.Title, "reviewers": reviewers}, nil)
	return err
}

func (p pulls) RequestReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error {
	return p.setReviewers(ctx, owner, repo, number, users, teams, true)
}

func (p pulls) RemoveReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error {
	return p.setReviewers(ctx, owner, repo, number, users, teams, false)
}

// ListReviews reports each participant's verdict (approved or changes
// requested); Bitbucket has no review objects, so IDs are 0 and comments are
// not included.
func (p pulls) ListReviews(ctx context.Context, owner, repo string, number int, _ gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Review], error) {
	cur, err := p.get(ctx, owner, repo, number)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Review]{}, err
	}
	page := gitoperationslib.Page[gitoperationslib.Review]{Items: []gitoperationslib.Review{}}
	for _, part := range cur.Participants {
		var state gitoperationslib.ReviewState
		switch {
		case part.Approved:
			state = gitoperationslib.ReviewApproved
		case part.State != nil && *part.State == "changes_requested":
			state = gitoperationslib.ReviewChangesRequested
		default:
			continue
		}
		r := gitoperationslib.Review{Reviewer: part.User.login(), State: state}
		if part.ParticipatedOn != nil {
			r.SubmittedAt = *part.ParticipatedOn
		}
		page.Items = append(page.Items, r)
	}
	return page, nil
}

// CreateReview approves, requests changes or comments. Bitbucket ties
// approvals to the pull request, not to a commit: CommitID is not supported.
func (p pulls) CreateReview(ctx context.Context, owner, repo string, number int, opts gitoperationslib.CreateReviewOptions) (*gitoperationslib.Review, error) {
	const op = "pulls.create_review"
	if opts.CommitID != "" {
		return nil, notSupported(op, "reviewing a specific commit")
	}
	me, err := p.c.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	n := strconv.Itoa(number)
	review := &gitoperationslib.Review{Reviewer: me.login(), Body: opts.Body, SubmittedAt: time.Now()}
	switch opts.Event {
	case gitoperationslib.ReviewEventApprove:
		review.State = gitoperationslib.ReviewApproved
		_, err = p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, n, "approve"), nil, nil, nil)
	case gitoperationslib.ReviewEventRequestChanges:
		review.State = gitoperationslib.ReviewChangesRequested
		_, err = p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, n, "request-changes"), nil, nil, nil)
	case gitoperationslib.ReviewEventComment:
		review.State = gitoperationslib.ReviewCommented
		if opts.Body == "" {
			return nil, errors.New("bitbucket: " + op + ": a comment needs a body")
		}
	default:
		return nil, fmt.Errorf("bitbucket: %s: invalid event %q", op, opts.Event)
	}
	if err != nil {
		return nil, err
	}
	if opts.Body != "" {
		var c struct {
			ID int64 `json:"id"`
		}
		if _, err := p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, n, "comments"), nil,
			map[string]any{"content": map[string]string{"raw": opts.Body}}, &c); err != nil {
			return nil, err
		}
		review.ID = c.ID
	}
	return review, nil
}
