package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func toBranch(b *gh.PullRequestBranch) gitoperationslib.PRBranch {
	return gitoperationslib.PRBranch{Ref: b.GetRef(), SHA: b.GetSHA(), Repo: b.GetRepo().GetFullName()}
}

func toPR(p *gh.PullRequest) gitoperationslib.PullRequest {
	if p == nil {
		return gitoperationslib.PullRequest{}
	}
	return gitoperationslib.PullRequest{
		Number:         p.GetNumber(),
		Title:          p.GetTitle(),
		Body:           p.GetBody(),
		State:          gitoperationslib.PRState(p.GetState()),
		Draft:          p.GetDraft(),
		Author:         p.GetUser().GetLogin(),
		Head:           toBranch(p.GetHead()),
		Base:           toBranch(p.GetBase()),
		Mergeable:      p.Mergeable, // nil until GitHub has computed it
		Merged:         p.GetMerged(),
		MergedBy:       p.GetMergedBy().GetLogin(),
		MergedAt:       p.GetMergedAt().Time,
		MergeCommitSHA: p.GetMergeCommitSHA(),
		HTMLURL:        p.GetHTMLURL(),
		CreatedAt:      p.GetCreatedAt().Time,
		UpdatedAt:      p.GetUpdatedAt().Time,
		ClosedAt:       p.GetClosedAt().Time,
	}
}

var reviewStates = map[string]gitoperationslib.ReviewState{
	"APPROVED":          gitoperationslib.ReviewApproved,
	"CHANGES_REQUESTED": gitoperationslib.ReviewChangesRequested,
	"COMMENTED":         gitoperationslib.ReviewCommented,
	"PENDING":           gitoperationslib.ReviewPending,
	"DISMISSED":         gitoperationslib.ReviewDismissed,
}

func toReview(r *gh.PullRequestReview) gitoperationslib.Review {
	state, ok := reviewStates[r.GetState()]
	if !ok {
		state = gitoperationslib.ReviewState(strings.ToLower(r.GetState()))
	}
	return gitoperationslib.Review{
		ID:          r.GetID(),
		Reviewer:    r.GetUser().GetLogin(),
		State:       state,
		Body:        r.GetBody(),
		CommitID:    r.GetCommitID(),
		HTMLURL:     r.GetHTMLURL(),
		SubmittedAt: r.GetSubmittedAt().Time,
	}
}

var reviewEvents = map[gitoperationslib.ReviewEvent]string{
	gitoperationslib.ReviewEventApprove:        "APPROVE",
	gitoperationslib.ReviewEventRequestChanges: "REQUEST_CHANGES",
	gitoperationslib.ReviewEventComment:        "COMMENT",
}

// PullRequests implements gitoperationslib.Client.
func (c *Client) PullRequests() gitoperationslib.PullRequests { return pulls{c} }

type pulls struct{ c *Client }

func (p pulls) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListPROptions) (gitoperationslib.Page[gitoperationslib.PullRequest], error) {
	list, resp, err := p.c.gh.PullRequests.List(ctx, owner, repo, &gh.PullRequestListOptions{
		State:       string(opts.State),
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.PullRequest]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.PullRequest]{Items: make([]gitoperationslib.PullRequest, len(list)), NextPage: resp.NextPage}
	for i, pr := range list {
		page.Items[i] = toPR(pr)
	}
	return page, nil
}

func singlePR(pr *gh.PullRequest, _ *gh.Response, err error) (*gitoperationslib.PullRequest, error) {
	if err != nil {
		return nil, convertError(err)
	}
	out := toPR(pr)
	return &out, nil
}

func (p pulls) Get(ctx context.Context, owner, repo string, number int) (*gitoperationslib.PullRequest, error) {
	return singlePR(p.c.gh.PullRequests.Get(ctx, owner, repo, number))
}

func (p pulls) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreatePROptions) (*gitoperationslib.PullRequest, error) {
	if opts.Title == "" || opts.Head == "" || opts.Base == "" {
		return nil, errors.New("github: pulls.create: title, head and base are required")
	}
	return singlePR(p.c.gh.PullRequests.Create(ctx, owner, repo, gh.CreatePullRequest{
		Title:               &opts.Title,
		Body:                nonEmpty(opts.Body),
		Head:                opts.Head,
		Base:                opts.Base,
		Draft:               gh.Ptr(opts.Draft),
		MaintainerCanModify: opts.AllowMaintainerEdit,
	}))
}

func (p pulls) Edit(ctx context.Context, owner, repo string, number int, opts gitoperationslib.EditPROptions) (*gitoperationslib.PullRequest, error) {
	const op = "pulls.edit"
	body := &gh.PullRequest{
		Title:               opts.Title,
		Body:                opts.Body,
		MaintainerCanModify: opts.AllowMaintainerEdit,
	}
	if opts.Base != nil {
		body.Base = &gh.PullRequestBranch{Ref: opts.Base}
	}
	if opts.State != nil {
		if *opts.State != gitoperationslib.PRStateOpen && *opts.State != gitoperationslib.PRStateClosed {
			return nil, fmt.Errorf("github: %s: invalid state %q", op, *opts.State)
		}
		body.State = (*string)(opts.State)
	}
	restChange := opts.Title != nil || opts.Body != nil || opts.Base != nil || opts.State != nil || opts.AllowMaintainerEdit != nil

	var pr *gh.PullRequest
	var err error
	if restChange {
		if pr, _, err = p.c.gh.PullRequests.Edit(ctx, owner, repo, number, body); err != nil {
			return nil, convertError(err)
		}
	}
	// Draft status can only be changed through GraphQL.
	if opts.Draft != nil {
		if pr == nil {
			if pr, _, err = p.c.gh.PullRequests.Get(ctx, owner, repo, number); err != nil {
				return nil, convertError(err)
			}
		}
		if pr.GetDraft() != *opts.Draft {
			mutation := `mutation($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { clientMutationId } }`
			if *opts.Draft {
				mutation = `mutation($id: ID!) { convertPullRequestToDraft(input: {pullRequestId: $id}) { clientMutationId } }`
			}
			if err := p.c.graphql(ctx, mutation, map[string]any{"id": pr.GetNodeID()}, nil); err != nil {
				return nil, fmt.Errorf("%s: %w", op, err)
			}
			if pr, _, err = p.c.gh.PullRequests.Get(ctx, owner, repo, number); err != nil {
				return nil, convertError(err)
			}
		}
	}
	if pr == nil { // nothing to change
		if pr, _, err = p.c.gh.PullRequests.Get(ctx, owner, repo, number); err != nil {
			return nil, convertError(err)
		}
	}
	out := toPR(pr)
	return &out, nil
}

func (p pulls) Merge(ctx context.Context, owner, repo string, number int, opts gitoperationslib.MergeOptions) error {
	const op = "pulls.merge"
	method := opts.Method
	if method == "" {
		method = gitoperationslib.MergeMethodMerge
	}
	switch method {
	case gitoperationslib.MergeMethodMerge, gitoperationslib.MergeMethodSquash, gitoperationslib.MergeMethodRebase:
	default:
		if !method.Valid() {
			return fmt.Errorf("github: %s: invalid merge method %q", op, method)
		}
		return gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, fmt.Sprintf("merge method %q", method))
	}
	if opts.WhenChecksSucceed {
		return p.enableAutoMerge(ctx, owner, repo, number, method, opts)
	}

	var head string
	if opts.DeleteBranch {
		pr, _, err := p.c.gh.PullRequests.Get(ctx, owner, repo, number)
		if err != nil {
			return convertError(err)
		}
		if pr.GetHead().GetRepo().GetFullName() != owner+"/"+repo {
			return gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "deleting a head branch in a fork")
		}
		head = pr.GetHead().GetRef()
	}
	_, _, err := p.c.gh.PullRequests.Merge(ctx, owner, repo, number, opts.Message, &gh.PullRequestOptions{
		CommitTitle: opts.Title,
		SHA:         opts.HeadSHA,
		MergeMethod: string(method),
	})
	if err != nil {
		return convertError(err)
	}
	if head != "" {
		if _, err := p.c.gh.Git.DeleteRef(ctx, owner, repo, "heads/"+head); err != nil {
			return fmt.Errorf("github: %s: merged, but deleting branch %q failed: %w", op, head, convertError(err))
		}
	}
	return nil
}

// enableAutoMerge schedules the merge through GraphQL. GitHub accepts it
// only while the pull request is waiting for something (e.g. a required
// status check), and only in repositories that allow auto-merge.
func (p pulls) enableAutoMerge(ctx context.Context, owner, repo string, number int, method gitoperationslib.MergeMethod, opts gitoperationslib.MergeOptions) error {
	const op = "pulls.merge"
	if opts.DeleteBranch {
		return gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op,
			"deleting the branch after an auto-merge (use the repository's delete-branch-on-merge setting)")
	}
	pr, _, err := p.c.gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return convertError(err)
	}
	vars := map[string]any{"id": pr.GetNodeID(), "method": strings.ToUpper(string(method))}
	if opts.Title != "" {
		vars["title"] = opts.Title
	}
	if opts.Message != "" {
		vars["body"] = opts.Message
	}
	if opts.HeadSHA != "" {
		vars["sha"] = opts.HeadSHA
	}
	const mutation = `mutation($id: ID!, $method: PullRequestMergeMethod!, $title: String, $body: String, $sha: GitObjectID) {
  enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: $method, commitHeadline: $title, commitBody: $body, expectedHeadOid: $sha}) { clientMutationId }
}`
	if err := p.c.graphql(ctx, mutation, vars, nil); err != nil {
		return fmt.Errorf("%s: auto-merge: %w", op, err)
	}
	return nil
}

func (p pulls) CancelAutoMerge(ctx context.Context, owner, repo string, number int) error {
	pr, _, err := p.c.gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return convertError(err)
	}
	const mutation = `mutation($id: ID!) { disablePullRequestAutoMerge(input: {pullRequestId: $id}) { clientMutationId } }`
	if err := p.c.graphql(ctx, mutation, map[string]any{"id": pr.GetNodeID()}, nil); err != nil {
		return fmt.Errorf("pulls.cancel_auto_merge: %w", err)
	}
	return nil
}

func (p pulls) IsMerged(ctx context.Context, owner, repo string, number int) (bool, error) {
	merged, _, err := p.c.gh.PullRequests.IsMerged(ctx, owner, repo, number)
	return merged, convertError(err)
}

func (p pulls) RequestReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error {
	_, _, err := p.c.gh.PullRequests.RequestReviewers(ctx, owner, repo, number, gh.ReviewersRequest{Reviewers: users, TeamReviewers: teams})
	return convertError(err)
}

func (p pulls) RemoveReviewers(ctx context.Context, owner, repo string, number int, users, teams []string) error {
	_, err := p.c.gh.PullRequests.RemoveReviewers(ctx, owner, repo, number, gh.ReviewersRequest{Reviewers: users, TeamReviewers: teams})
	return convertError(err)
}

func (p pulls) ListReviews(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Review], error) {
	list, resp, err := p.c.gh.PullRequests.ListReviews(ctx, owner, repo, number, &gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Review]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Review]{Items: make([]gitoperationslib.Review, len(list)), NextPage: resp.NextPage}
	for i, r := range list {
		page.Items[i] = toReview(r)
	}
	return page, nil
}

func (p pulls) CreateReview(ctx context.Context, owner, repo string, number int, opts gitoperationslib.CreateReviewOptions) (*gitoperationslib.Review, error) {
	event, ok := reviewEvents[opts.Event]
	if !ok {
		return nil, fmt.Errorf("github: pulls.create_review: invalid event %q", opts.Event)
	}
	r, _, err := p.c.gh.PullRequests.CreateReview(ctx, owner, repo, number, &gh.PullRequestReviewRequest{
		Event:    &event,
		Body:     nonEmpty(opts.Body),
		CommitID: nonEmpty(opts.CommitID),
	})
	if err != nil {
		return nil, convertError(err)
	}
	review := toReview(r)
	return &review, nil
}
