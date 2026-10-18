package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func toLabel(l *gh.Label) gitoperationslib.Label {
	return gitoperationslib.Label{ID: l.GetID(), Name: l.GetName(), Color: strings.ToLower(l.GetColor()), Description: l.GetDescription()}
}

func toMilestone(m *gh.Milestone) gitoperationslib.Milestone {
	return gitoperationslib.Milestone{
		ID: int64(m.GetNumber()), Title: m.GetTitle(), Description: m.GetDescription(), State: gitoperationslib.MilestoneState(m.GetState()),
		DueOn: m.GetDueOn().Time, OpenIssues: m.GetOpenIssues(), ClosedIssues: m.GetClosedIssues(), ClosedAt: m.GetClosedAt().Time,
	}
}

func toIssue(i *gh.Issue) gitoperationslib.Issue {
	issue := gitoperationslib.Issue{
		Number: i.GetNumber(), Title: i.GetTitle(), Body: i.GetBody(), State: gitoperationslib.IssueState(i.GetState()),
		Author: i.GetUser().GetLogin(), Labels: []string{}, Assignees: []string{}, Comments: i.GetComments(), Locked: i.GetLocked(),
		HTMLURL: i.GetHTMLURL(), CreatedAt: i.GetCreatedAt().Time, UpdatedAt: i.GetUpdatedAt().Time, ClosedAt: i.GetClosedAt().Time,
	}
	for _, l := range i.Labels {
		issue.Labels = append(issue.Labels, l.GetName())
	}
	for _, a := range i.Assignees {
		issue.Assignees = append(issue.Assignees, a.GetLogin())
	}
	if i.Milestone != nil {
		m := toMilestone(i.Milestone)
		issue.Milestone = &m
	}
	return issue
}

func toComment(c *gh.IssueComment) gitoperationslib.Comment {
	return gitoperationslib.Comment{
		ID: c.GetID(), Author: c.GetUser().GetLogin(), Body: c.GetBody(), HTMLURL: c.GetHTMLURL(),
		CreatedAt: c.GetCreatedAt().Time, UpdatedAt: c.GetUpdatedAt().Time,
	}
}

// Issues implements gitoperationslib.Client.
func (c *Client) Issues() gitoperationslib.Issues { return issues{c} }

type issues struct{ c *Client }

// List excludes pull requests, which GitHub's issue list includes. A page
// may therefore hold fewer items than PerPage.
func (s issues) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListIssuesOptions) (gitoperationslib.Page[gitoperationslib.Issue], error) {
	list, resp, err := s.c.gh.Issues.ListByRepo(ctx, owner, repo, &gh.IssueListByRepoOptions{
		State: string(opts.State), Labels: opts.Labels,
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Issue]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Issue]{Items: []gitoperationslib.Issue{}, NextPage: resp.NextPage}
	for _, i := range list {
		if i.IsPullRequest() {
			continue
		}
		page.Items = append(page.Items, toIssue(i))
	}
	return page, nil
}

func singleIssue(i *gh.Issue, _ *gh.Response, err error) (*gitoperationslib.Issue, error) {
	if err != nil {
		return nil, convertError(err)
	}
	out := toIssue(i)
	return &out, nil
}

func (s issues) Get(ctx context.Context, owner, repo string, number int) (*gitoperationslib.Issue, error) {
	return singleIssue(s.c.gh.Issues.Get(ctx, owner, repo, number))
}

func (s issues) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateIssueOptions) (*gitoperationslib.Issue, error) {
	if opts.Title == "" {
		return nil, errors.New("github: issues.create: title is required")
	}
	req := gh.CreateIssueRequest{Title: opts.Title, Body: nonEmpty(opts.Body), Labels: opts.Labels, Assignees: opts.Assignees}
	if opts.Milestone != 0 {
		req.Milestone = gh.Ptr(int(opts.Milestone))
	}
	return singleIssue(s.c.gh.Issues.Create(ctx, owner, repo, req))
}

func (s issues) Edit(ctx context.Context, owner, repo string, number int, opts gitoperationslib.EditIssueOptions) (*gitoperationslib.Issue, error) {
	req := gh.UpdateIssueRequest{Title: opts.Title, Body: opts.Body}
	if opts.State != nil {
		if *opts.State != gitoperationslib.IssueStateOpen && *opts.State != gitoperationslib.IssueStateClosed {
			return nil, fmt.Errorf("github: issues.edit: invalid state %q", *opts.State)
		}
		req.State = (*string)(opts.State)
	}
	if opts.Labels != nil {
		req.Labels = nonNilStrings(*opts.Labels)
	}
	if opts.Assignees != nil {
		req.Assignees = nonNilStrings(*opts.Assignees)
	}
	if opts.Milestone != nil {
		if *opts.Milestone == 0 {
			// UpdateIssueRequest cannot send "milestone": null; clear it
			// with a raw request.
			if err := s.clearMilestone(ctx, owner, repo, number); err != nil {
				return nil, err
			}
		} else {
			req.Milestone = gh.Ptr(int(*opts.Milestone))
		}
	}
	return singleIssue(s.c.gh.Issues.Update(ctx, owner, repo, number, req))
}

func (s issues) clearMilestone(ctx context.Context, owner, repo string, number int) error {
	req, err := s.c.gh.NewRequest(ctx, "PATCH", fmt.Sprintf("repos/%s/%s/issues/%d", owner, repo, number), map[string]any{"milestone": nil})
	if err != nil {
		return fmt.Errorf("github: issues.edit: %w", err)
	}
	_, err = s.c.gh.Do(req, nil)
	return convertError(err)
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Delete removes the issue through GraphQL (REST cannot delete issues).
func (s issues) Delete(ctx context.Context, owner, repo string, number int) error {
	issue, _, err := s.c.gh.Issues.Get(ctx, owner, repo, number)
	if err != nil {
		return convertError(err)
	}
	const mutation = `mutation($id: ID!) { deleteIssue(input: {issueId: $id}) { clientMutationId } }`
	if err := s.c.graphql(ctx, mutation, map[string]any{"id": issue.GetNodeID()}, nil); err != nil {
		return fmt.Errorf("issues.delete: %w", err)
	}
	return nil
}

func (s issues) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	_, _, err := s.c.gh.Issues.AddLabelsToIssue(ctx, owner, repo, number, labels)
	return convertError(err)
}

func (s issues) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	_, err := s.c.gh.Issues.RemoveLabelForIssue(ctx, owner, repo, number, label)
	return convertError(err)
}

func (s issues) ListComments(ctx context.Context, owner, repo string, number int, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Comment], error) {
	list, resp, err := s.c.gh.Issues.ListComments(ctx, owner, repo, number, &gh.IssueListCommentsOptions{
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Comment]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Comment]{Items: make([]gitoperationslib.Comment, len(list)), NextPage: resp.NextPage}
	for i, c := range list {
		page.Items[i] = toComment(c)
	}
	return page, nil
}

func singleComment(c *gh.IssueComment, _ *gh.Response, err error) (*gitoperationslib.Comment, error) {
	if err != nil {
		return nil, convertError(err)
	}
	out := toComment(c)
	return &out, nil
}

func (s issues) CreateComment(ctx context.Context, owner, repo string, number int, body string) (*gitoperationslib.Comment, error) {
	return singleComment(s.c.gh.Issues.CreateComment(ctx, owner, repo, number, gh.IssueCommentRequest{Body: body}))
}

func (s issues) EditComment(ctx context.Context, owner, repo string, id int64, body string) (*gitoperationslib.Comment, error) {
	return singleComment(s.c.gh.Issues.UpdateComment(ctx, owner, repo, id, gh.IssueCommentRequest{Body: body}))
}

func (s issues) DeleteComment(ctx context.Context, owner, repo string, id int64) error {
	_, err := s.c.gh.Issues.DeleteComment(ctx, owner, repo, id)
	return convertError(err)
}

// Labels implements gitoperationslib.Client.
func (c *Client) Labels() gitoperationslib.Labels { return labels{c} }

type labels struct{ c *Client }

func (l labels) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Label], error) {
	list, resp, err := l.c.gh.Issues.ListLabels(ctx, owner, repo, &gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Label]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Label]{Items: make([]gitoperationslib.Label, len(list)), NextPage: resp.NextPage}
	for i, lb := range list {
		page.Items[i] = toLabel(lb)
	}
	return page, nil
}

func singleLabel(lb *gh.Label, _ *gh.Response, err error) (*gitoperationslib.Label, error) {
	if err != nil {
		return nil, convertError(err)
	}
	out := toLabel(lb)
	return &out, nil
}

func (l labels) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Label, error) {
	return singleLabel(l.c.gh.Issues.GetLabel(ctx, owner, repo, name))
}

func (l labels) Create(ctx context.Context, owner, repo string, label gitoperationslib.Label) (*gitoperationslib.Label, error) {
	if label.Name == "" {
		return nil, errors.New("github: labels.create: name is required")
	}
	return singleLabel(l.c.gh.Issues.CreateLabel(ctx, owner, repo, gh.CreateIssueLabelRequest{
		Name: label.Name, Color: nonEmpty(strings.TrimPrefix(label.Color, "#")), Description: nonEmpty(label.Description),
	}))
}

func (l labels) Edit(ctx context.Context, owner, repo, name string, opts gitoperationslib.EditLabelOptions) (*gitoperationslib.Label, error) {
	req := gh.UpdateIssueLabelRequest{NewName: opts.Name, Description: opts.Description}
	if opts.Color != nil {
		req.Color = gh.Ptr(strings.TrimPrefix(*opts.Color, "#"))
	}
	return singleLabel(l.c.gh.Issues.UpdateLabel(ctx, owner, repo, name, req))
}

func (l labels) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := l.c.gh.Issues.DeleteLabel(ctx, owner, repo, name)
	return convertError(err)
}

// Milestones implements gitoperationslib.Client.
func (c *Client) Milestones() gitoperationslib.Milestones { return milestones{c} }

type milestones struct{ c *Client }

func (m milestones) List(ctx context.Context, owner, repo string, state gitoperationslib.MilestoneState, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Milestone], error) {
	list, resp, err := m.c.gh.Issues.ListMilestones(ctx, owner, repo, &gh.MilestoneListOptions{
		State: string(state), ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Milestone]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Milestone]{Items: make([]gitoperationslib.Milestone, len(list)), NextPage: resp.NextPage}
	for i, ms := range list {
		page.Items[i] = toMilestone(ms)
	}
	return page, nil
}

func singleMilestone(ms *gh.Milestone, _ *gh.Response, err error) (*gitoperationslib.Milestone, error) {
	if err != nil {
		return nil, convertError(err)
	}
	out := toMilestone(ms)
	return &out, nil
}

func (m milestones) Get(ctx context.Context, owner, repo string, id int64) (*gitoperationslib.Milestone, error) {
	return singleMilestone(m.c.gh.Issues.GetMilestone(ctx, owner, repo, int(id)))
}

func (m milestones) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateMilestoneOptions) (*gitoperationslib.Milestone, error) {
	if opts.Title == "" {
		return nil, errors.New("github: milestones.create: title is required")
	}
	req := gh.CreateMilestoneRequest{Title: opts.Title, Description: nonEmpty(opts.Description)}
	if !opts.DueOn.IsZero() {
		req.DueOn = &gh.Timestamp{Time: opts.DueOn}
	}
	return singleMilestone(m.c.gh.Issues.CreateMilestone(ctx, owner, repo, req))
}

func (m milestones) Edit(ctx context.Context, owner, repo string, id int64, opts gitoperationslib.EditMilestoneOptions) (*gitoperationslib.Milestone, error) {
	req := gh.UpdateMilestoneRequest{Title: opts.Title, Description: opts.Description, State: (*string)(opts.State)}
	if opts.DueOn != nil {
		req.DueOn = &gh.Timestamp{Time: *opts.DueOn}
	}
	return singleMilestone(m.c.gh.Issues.UpdateMilestone(ctx, owner, repo, int(id), req))
}

func (m milestones) Delete(ctx context.Context, owner, repo string, id int64) error {
	_, err := m.c.gh.Issues.DeleteMilestone(ctx, owner, repo, int(id))
	return convertError(err)
}
