package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type loginJSON struct {
	Login string `json:"login"`
}

func (l *loginJSON) login() string {
	if l == nil {
		return ""
	}
	return l.Login
}

type prBranchJSON struct {
	Label string `json:"label"` // the branch name
	Ref   string `json:"ref"`
	SHA   string `json:"sha"`
	Repo  *struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
}

func (b *prBranchJSON) toBranch() gitoperationslib.PRBranch {
	// Once the head branch is deleted (after a merge, typically), Forgejo reports the pull request's own ref
	// (refs/pull/N/head) as ref; the label still holds the branch name.
	ref := b.Ref
	if strings.HasPrefix(ref, "refs/pull/") && b.Label != "" {
		ref = b.Label
	}
	br := gitoperationslib.PRBranch{Ref: ref, SHA: b.SHA}
	if b.Repo != nil {
		br.Repo = b.Repo.FullName
	}
	return br
}

type pullJSON struct {
	Number         int          `json:"number"`
	Title          string       `json:"title"`
	Body           string       `json:"body"`
	State          string       `json:"state"`
	Draft          bool         `json:"draft"`
	User           *loginJSON   `json:"user"`
	Head           prBranchJSON `json:"head"`
	Base           prBranchJSON `json:"base"`
	Mergeable      bool         `json:"mergeable"`
	Merged         bool         `json:"merged"`
	MergedBy       *loginJSON   `json:"merged_by"`
	MergedAt       *time.Time   `json:"merged_at"`
	MergeCommitSHA string       `json:"merge_commit_sha"`
	HTMLURL        string       `json:"html_url"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	ClosedAt       *time.Time   `json:"closed_at"`
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func (p *pullJSON) toPR() gitoperationslib.PullRequest {
	return gitoperationslib.PullRequest{
		Number:         p.Number,
		Title:          p.Title,
		Body:           p.Body,
		State:          gitoperationslib.PRState(p.State),
		Draft:          p.Draft,
		Author:         p.User.login(),
		Head:           p.Head.toBranch(),
		Base:           p.Base.toBranch(),
		Mergeable:      gitoperationslib.Ptr(p.Mergeable), // Forgejo always reports it
		Merged:         p.Merged,
		MergedBy:       p.MergedBy.login(),
		MergedAt:       timeOrZero(p.MergedAt),
		MergeCommitSHA: p.MergeCommitSHA,
		HTMLURL:        p.HTMLURL,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
		ClosedAt:       timeOrZero(p.ClosedAt),
	}
}

type reviewJSON struct {
	ID          int64      `json:"id"`
	User        *loginJSON `json:"user"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	CommitID    string     `json:"commit_id"`
	HTMLURL     string     `json:"html_url"`
	Dismissed   bool       `json:"dismissed"`
	SubmittedAt *time.Time `json:"submitted_at"`
}

var reviewStates = map[string]gitoperationslib.ReviewState{
	"APPROVED":        gitoperationslib.ReviewApproved,
	"REQUEST_CHANGES": gitoperationslib.ReviewChangesRequested,
	"COMMENT":         gitoperationslib.ReviewCommented,
	"PENDING":         gitoperationslib.ReviewPending,
	"REQUEST_REVIEW":  gitoperationslib.ReviewRequested,
}

func (r *reviewJSON) toReview() gitoperationslib.Review {
	state, ok := reviewStates[r.State]
	if !ok {
		state = gitoperationslib.ReviewState(strings.ToLower(r.State))
	}
	if r.Dismissed {
		state = gitoperationslib.ReviewDismissed
	}
	return gitoperationslib.Review{
		ID:          r.ID,
		Reviewer:    r.User.login(),
		State:       state,
		Body:        r.Body,
		CommitID:    r.CommitID,
		HTMLURL:     r.HTMLURL,
		SubmittedAt: timeOrZero(r.SubmittedAt),
	}
}

var reviewEvents = map[gitoperationslib.ReviewEvent]string{
	gitoperationslib.ReviewEventApprove:        "APPROVED",
	gitoperationslib.ReviewEventRequestChanges: "REQUEST_CHANGES",
	gitoperationslib.ReviewEventComment:        "COMMENT",
}

// Forgejo's default WORK_IN_PROGRESS_PREFIXES; matched case-insensitively.
var wipPrefixes = []string{"WIP:", "[WIP]"}

func wipPrefix(title string) (string, bool) {
	for _, p := range wipPrefixes {
		if len(title) >= len(p) && strings.EqualFold(title[:len(p)], p) {
			return title[:len(p)], true
		}
	}
	return "", false
}

// draftTitle adds or removes the work-in-progress prefix.
func draftTitle(title string, draft bool) string {
	p, has := wipPrefix(title)
	switch {
	case draft && !has:
		return "WIP: " + title
	case !draft && has:
		return strings.TrimLeft(title[len(p):], " ")
	}
	return title
}

func pullsPath(owner, repo string, rest ...string) string {
	return forgejoapi.Path(append([]string{"repos", owner, repo, "pulls"}, rest...)...)
}

// PullRequests implements gitoperationslib.Client.
func (c *Client) PullRequests() gitoperationslib.PullRequests { return pulls{c} }

type pulls struct{ c *Client }

func (p pulls) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListPROptions) (gitoperationslib.Page[gitoperationslib.PullRequest], error) {
	q := forgejoapi.ListQuery(opts.ListOptions)
	if opts.State != "" {
		q.Set("state", string(opts.State))
	}
	var out []pullJSON
	resp, err := p.c.api.Do(ctx, http.MethodGet, pullsPath(owner, repo), q, nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.PullRequest]{}, err
	}
	return toPage(resp, out, (*pullJSON).toPR), nil
}

func (p pulls) Get(ctx context.Context, owner, repo string, number int) (*gitoperationslib.PullRequest, error) {
	var out pullJSON
	if _, err := p.c.api.Do(ctx, http.MethodGet, pullsPath(owner, repo, strconv.Itoa(number)), nil, nil, &out); err != nil {
		return nil, err
	}
	pr := out.toPR()
	return &pr, nil
}

// checkDraft reports a Draft request the server did not honour.
func checkDraft(pr *gitoperationslib.PullRequest, want bool, op string) (*gitoperationslib.PullRequest, error) {
	if pr.Draft != want {
		return pr, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op,
			fmt.Sprintf("server reports draft=%v for title %q; the instance may use other work-in-progress prefixes", pr.Draft, pr.Title))
	}
	return pr, nil
}

func (p pulls) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreatePROptions) (*gitoperationslib.PullRequest, error) {
	const op = "pulls.create"
	if opts.Title == "" || opts.Head == "" || opts.Base == "" {
		return nil, errors.New("forgejo: pulls.create: title, head and base are required")
	}
	title := opts.Title
	if opts.Draft {
		title = draftTitle(title, true)
	}
	body := struct {
		Title string `json:"title"`
		Body  string `json:"body,omitempty"`
		Head  string `json:"head"`
		Base  string `json:"base"`
	}{title, opts.Body, opts.Head, opts.Base}
	var out pullJSON
	if _, err := p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo), nil, body, &out); err != nil {
		return nil, err
	}
	pr := out.toPR()
	// Forgejo has no allow_maintainer_edit on create; apply it with an edit.
	if opts.AllowMaintainerEdit != nil {
		edited, err := p.Edit(ctx, owner, repo, pr.Number, gitoperationslib.EditPROptions{AllowMaintainerEdit: opts.AllowMaintainerEdit})
		if err != nil {
			return &pr, fmt.Errorf("forgejo: %s: created #%d but could not set maintainer edits: %w", op, pr.Number, err)
		}
		pr = *edited
	}
	if opts.Draft {
		return checkDraft(&pr, true, op)
	}
	return &pr, nil
}

func (p pulls) Edit(ctx context.Context, owner, repo string, number int, opts gitoperationslib.EditPROptions) (*gitoperationslib.PullRequest, error) {
	const op = "pulls.edit"
	body := struct {
		Title               *string `json:"title,omitempty"`
		Body                *string `json:"body,omitempty"`
		Base                *string `json:"base,omitempty"`
		State               *string `json:"state,omitempty"`
		AllowMaintainerEdit *bool   `json:"allow_maintainer_edit,omitempty"`
	}{Title: opts.Title, Body: opts.Body, Base: opts.Base, AllowMaintainerEdit: opts.AllowMaintainerEdit}
	if opts.State != nil {
		if *opts.State != gitoperationslib.PRStateOpen && *opts.State != gitoperationslib.PRStateClosed {
			return nil, fmt.Errorf("forgejo: %s: invalid state %q", op, *opts.State)
		}
		body.State = (*string)(opts.State)
	}
	if opts.Draft != nil {
		title := ""
		if opts.Title != nil {
			title = *opts.Title
		} else {
			cur, err := p.Get(ctx, owner, repo, number)
			if err != nil {
				return nil, err
			}
			title = cur.Title
		}
		title = draftTitle(title, *opts.Draft)
		body.Title = &title
	}
	var out pullJSON
	if _, err := p.c.api.Do(ctx, http.MethodPatch, pullsPath(owner, repo, strconv.Itoa(number)), nil, body, &out); err != nil {
		return nil, err
	}
	pr := out.toPR()
	if opts.Draft != nil {
		return checkDraft(&pr, *opts.Draft, op)
	}
	return &pr, nil
}

func (p pulls) Merge(ctx context.Context, owner, repo string, number int, opts gitoperationslib.MergeOptions) error {
	method := opts.Method
	if method == "" {
		method = gitoperationslib.MergeMethodMerge
	}
	if !method.Valid() {
		return fmt.Errorf("forgejo: pulls.merge: invalid merge method %q", method)
	}
	body := struct {
		Do                string `json:"Do"`
		Title             string `json:"MergeTitleField,omitempty"`
		Message           string `json:"MergeMessageField,omitempty"`
		HeadSHA           string `json:"head_commit_id,omitempty"`
		DeleteBranch      bool   `json:"delete_branch_after_merge,omitempty"`
		WhenChecksSucceed bool   `json:"merge_when_checks_succeed,omitempty"`
	}{string(method), opts.Title, opts.Message, opts.HeadSHA, opts.DeleteBranch, opts.WhenChecksSucceed}
	_, err := p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, strconv.Itoa(number), "merge"), nil, body, nil)
	return err
}

func (p pulls) CancelAutoMerge(ctx context.Context, owner, repo string, number int) error {
	_, err := p.c.api.Do(ctx, http.MethodDelete, pullsPath(owner, repo, strconv.Itoa(number), "merge"), nil, nil, nil)
	return err
}

func (p pulls) IsMerged(ctx context.Context, owner, repo string, number int) (bool, error) {
	_, err := p.c.api.Do(ctx, http.MethodGet, pullsPath(owner, repo, strconv.Itoa(number), "merge"), nil, nil, nil)
	if gitoperationslib.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (p pulls) reviewers(ctx context.Context, method, owner, repo string, number int, users, teams []string) error {
	body := struct {
		Reviewers     []string `json:"reviewers,omitempty"`
		TeamReviewers []string `json:"team_reviewers,omitempty"`
	}{users, teams}
	_, err := p.c.api.Do(ctx, method, pullsPath(owner, repo, strconv.Itoa(number), "requested_reviewers"), nil, body, nil)
	return err
}

func (p pulls) RequestReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error {
	return p.reviewers(ctx, http.MethodPost, owner, repo, number, users, teams)
}

func (p pulls) RemoveReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error {
	return p.reviewers(ctx, http.MethodDelete, owner, repo, number, users, teams)
}

func (p pulls) ListReviews(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Review], error) {
	var out []reviewJSON
	resp, err := p.c.api.Do(ctx, http.MethodGet, pullsPath(owner, repo, strconv.Itoa(number), "reviews"), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Review]{}, err
	}
	return toPage(resp, out, (*reviewJSON).toReview), nil
}

func (p pulls) CreateReview(ctx context.Context, owner, repo string, number int, opts gitoperationslib.CreateReviewOptions) (*gitoperationslib.Review, error) {
	event, ok := reviewEvents[opts.Event]
	if !ok {
		return nil, fmt.Errorf("forgejo: pulls.create_review: invalid event %q", opts.Event)
	}
	body := struct {
		Event    string `json:"event"`
		Body     string `json:"body,omitempty"`
		CommitID string `json:"commit_id,omitempty"`
	}{event, opts.Body, opts.CommitID}
	var out reviewJSON
	if _, err := p.c.api.Do(ctx, http.MethodPost, pullsPath(owner, repo, strconv.Itoa(number), "reviews"), nil, body, &out); err != nil {
		return nil, err
	}
	review := out.toReview()
	return &review, nil
}
