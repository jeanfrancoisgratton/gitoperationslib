package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var (
	_ gitoperationslib.WorkflowRuns      = (*Client)(nil)
	_ gitoperationslib.WorkflowArtifacts = (*Client)(nil)
)

func toWorkflowRun(r *gh.WorkflowRun) gitoperationslib.WorkflowRun {
	return gitoperationslib.WorkflowRun{
		ID: r.GetID(), Number: int64(r.GetRunNumber()), Title: r.GetDisplayTitle(), Workflow: path.Base(r.GetPath()), Event: r.GetEvent(),
		Branch: r.GetHeadBranch(), SHA: r.GetHeadSHA(), Status: r.GetStatus(), Conclusion: r.GetConclusion(), Actor: r.GetActor().GetLogin(),
		HTMLURL: r.GetHTMLURL(), CreatedAt: r.GetCreatedAt().Time, StartedAt: r.GetRunStartedAt().Time, UpdatedAt: r.GetUpdatedAt().Time,
	}
}

func toWorkflowJob(j *gh.WorkflowJob) gitoperationslib.WorkflowJob {
	job := gitoperationslib.WorkflowJob{
		ID: j.GetID(), RunID: j.GetRunID(), Attempt: int(j.GetRunAttempt()), Name: j.GetName(), Status: j.GetStatus(), Conclusion: j.GetConclusion(),
		Labels: j.Labels, HTMLURL: j.GetHTMLURL(), StartedAt: j.GetStartedAt().Time, CompletedAt: j.GetCompletedAt().Time,
	}
	for _, s := range j.Steps {
		job.Steps = append(job.Steps, gitoperationslib.WorkflowStep{
			Number: int(s.GetNumber()), Name: s.GetName(), Status: s.GetStatus(), Conclusion: s.GetConclusion(),
			StartedAt: s.GetStartedAt().Time, CompletedAt: s.GetCompletedAt().Time,
		})
	}
	return job
}

func toWorkflowArtifact(a *gh.Artifact) gitoperationslib.WorkflowArtifact {
	return gitoperationslib.WorkflowArtifact{
		ID: a.GetID(), Name: a.GetName(), Size: a.GetSizeInBytes(), RunID: a.GetWorkflowRun().GetID(), Expired: a.GetExpired(),
		CreatedAt: a.GetCreatedAt().Time, UpdatedAt: a.GetUpdatedAt().Time, ExpiresAt: a.GetExpiresAt().Time,
	}
}

func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo string, opts gitoperationslib.ListWorkflowRunsOptions) (gitoperationslib.Page[gitoperationslib.WorkflowRun], error) {
	empty := gitoperationslib.Page[gitoperationslib.WorkflowRun]{Items: []gitoperationslib.WorkflowRun{}}
	if opts.Number != 0 {
		return empty, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "workflows.list", "filtering runs by number")
	}
	query := &gh.ListWorkflowRunsOptions{
		Status: opts.Status, Event: opts.Event, Branch: opts.Branch, HeadSHA: opts.SHA,
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	var list *gh.WorkflowRuns
	var resp *gh.Response
	var err error
	if opts.Workflow != "" {
		list, resp, err = c.gh.Actions.ListWorkflowRunsByFileName(ctx, owner, repo, opts.Workflow, query)
		if err != nil {
			err = convertError(err)
			// A workflow that does not exist selects nothing, unless the repository
			// is what is missing.
			if gitoperationslib.IsNotFound(err) {
				if _, _, repoErr := c.gh.Repositories.Get(ctx, owner, repo); repoErr == nil {
					return empty, nil
				}
			}
			return gitoperationslib.Page[gitoperationslib.WorkflowRun]{}, err
		}
	} else {
		list, resp, err = c.gh.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, query)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.WorkflowRun]{}, convertError(err)
		}
	}
	page := gitoperationslib.Page[gitoperationslib.WorkflowRun]{Items: make([]gitoperationslib.WorkflowRun, len(list.WorkflowRuns)), NextPage: resp.NextPage}
	for i, r := range list.WorkflowRuns {
		page.Items[i] = toWorkflowRun(r)
	}
	return page, nil
}

func (c *Client) GetWorkflowRun(ctx context.Context, owner, repo string, id int64) (*gitoperationslib.WorkflowRun, error) {
	r, _, err := c.gh.Actions.GetWorkflowRunByID(ctx, owner, repo, id)
	if err != nil {
		return nil, convertError(err)
	}
	run := toWorkflowRun(r)
	return &run, nil
}

// accepted drops the error go-github returns for HTTP 202, which is a success.
func accepted(err error) error {
	var pending *gh.AcceptedError
	if errors.As(err, &pending) {
		return nil
	}
	return convertError(err)
}

// CancelWorkflowRun takes GitHub's 409 for a run already finished as success, as
// Forgejo answers.
func (c *Client) CancelWorkflowRun(ctx context.Context, owner, repo string, id int64) error {
	_, err := c.gh.Actions.CancelWorkflowRunByID(ctx, owner, repo, id)
	err = accepted(err)
	if gitoperationslib.StatusCode(err) == http.StatusConflict {
		return nil
	}
	return err
}

func (c *Client) RerunWorkflowRun(ctx context.Context, owner, repo string, id int64) error {
	_, err := c.gh.Actions.RerunWorkflowByID(ctx, owner, repo, id)
	return accepted(err)
}

func (c *Client) DeleteWorkflowRun(ctx context.Context, owner, repo string, id int64) error {
	run, err := c.GetWorkflowRun(ctx, owner, repo, id)
	if err != nil {
		return err
	}
	if run.Conclusion == "" {
		return fmt.Errorf("github: workflows.delete: run %d is %s: %w", id, run.Status, gitoperationslib.ErrWorkflowRunActive)
	}
	_, err = c.gh.Actions.DeleteWorkflowRun(ctx, owner, repo, id)
	return convertError(err)
}

func (c *Client) ListWorkflowJobs(ctx context.Context, owner, repo string, runID int64, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.WorkflowJob], error) {
	list, resp, err := c.gh.Actions.ListWorkflowJobs(ctx, owner, repo, runID, &gh.ListWorkflowJobsOptions{ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage}})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.WorkflowJob]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.WorkflowJob]{Items: make([]gitoperationslib.WorkflowJob, len(list.Jobs)), NextPage: resp.NextPage}
	for i, j := range list.Jobs {
		page.Items[i] = toWorkflowJob(j)
	}
	return page, nil
}

func (c *Client) GetWorkflowJob(ctx context.Context, owner, repo string, jobID int64) (*gitoperationslib.WorkflowJob, error) {
	j, _, err := c.gh.Actions.GetWorkflowJobByID(ctx, owner, repo, jobID)
	if err != nil {
		return nil, convertError(err)
	}
	job := toWorkflowJob(j)
	return &job, nil
}

func (c *Client) RerunWorkflowJob(ctx context.Context, owner, repo string, jobID int64) error {
	_, err := c.gh.Actions.RerunJobByID(ctx, owner, repo, jobID)
	return accepted(err)
}

// fetchRedirected downloads what GitHub redirects a log or artifact to. The address
// is signed, and on another host, so it goes without the token.
func (c *Client) fetchRedirected(ctx context.Context, target *url.URL, resp *gh.Response, err error) (io.ReadCloser, error) {
	if err != nil {
		// The SDK reports a status other than a redirect as a plain error, with the
		// response: a 404 must still read as one.
		var respErr *gh.ErrorResponse
		if resp != nil && resp.Response != nil && !errors.As(err, &respErr) {
			return nil, &gitoperationslib.APIError{Backend: gitoperationslib.BackendGitHub, StatusCode: resp.StatusCode, Message: err.Error()}
		}
		return nil, convertError(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	got, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if got.StatusCode != http.StatusOK {
		defer got.Body.Close()
		return nil, &gitoperationslib.APIError{Backend: gitoperationslib.BackendGitHub, StatusCode: got.StatusCode, Message: "downloading from " + target.Host}
	}
	return got.Body, nil
}

func (c *Client) WorkflowJobLogs(ctx context.Context, owner, repo string, jobID int64) (io.ReadCloser, error) {
	target, resp, err := c.gh.Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 2)
	return c.fetchRedirected(ctx, target, resp, err)
}

func (c *Client) WorkflowRunLogs(ctx context.Context, owner, repo string, runID int64) (io.ReadCloser, error) {
	target, resp, err := c.gh.Actions.GetWorkflowRunLogs(ctx, owner, repo, runID, 2)
	return c.fetchRedirected(ctx, target, resp, err)
}

func toArtifactPage(list *gh.ArtifactList, resp *gh.Response) gitoperationslib.Page[gitoperationslib.WorkflowArtifact] {
	page := gitoperationslib.Page[gitoperationslib.WorkflowArtifact]{Items: make([]gitoperationslib.WorkflowArtifact, len(list.Artifacts)), NextPage: resp.NextPage}
	for i, a := range list.Artifacts {
		page.Items[i] = toWorkflowArtifact(a)
	}
	return page
}

func (c *Client) ListWorkflowArtifacts(ctx context.Context, owner, repo string, opts gitoperationslib.ListWorkflowArtifactsOptions) (gitoperationslib.Page[gitoperationslib.WorkflowArtifact], error) {
	query := &gh.ListArtifactsOptions{ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage}}
	if opts.Name != "" {
		query.Name = &opts.Name
	}
	list, resp, err := c.gh.Actions.ListArtifacts(ctx, owner, repo, query)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.WorkflowArtifact]{}, convertError(err)
	}
	return toArtifactPage(list, resp), nil
}

// ListWorkflowRunArtifacts filters by name itself, per page: the SDK's call for a
// run takes no name. A page can hold fewer items than PerPage.
func (c *Client) ListWorkflowRunArtifacts(ctx context.Context, owner, repo string, runID int64, opts gitoperationslib.ListWorkflowArtifactsOptions) (gitoperationslib.Page[gitoperationslib.WorkflowArtifact], error) {
	list, resp, err := c.gh.Actions.ListWorkflowRunArtifacts(ctx, owner, repo, runID, &gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.WorkflowArtifact]{}, convertError(err)
	}
	page := toArtifactPage(list, resp)
	if opts.Name != "" {
		kept := page.Items[:0]
		for _, a := range page.Items {
			if a.Name == opts.Name {
				kept = append(kept, a)
			}
		}
		page.Items = kept
	}
	return page, nil
}

func (c *Client) GetWorkflowArtifact(ctx context.Context, owner, repo string, id int64) (*gitoperationslib.WorkflowArtifact, error) {
	a, _, err := c.gh.Actions.GetArtifact(ctx, owner, repo, id)
	if err != nil {
		return nil, convertError(err)
	}
	artifact := toWorkflowArtifact(a)
	return &artifact, nil
}

func (c *Client) DeleteWorkflowArtifact(ctx context.Context, owner, repo string, id int64) error {
	_, err := c.gh.Actions.DeleteArtifact(ctx, owner, repo, id)
	return convertError(err)
}

func (c *Client) DownloadWorkflowArtifact(ctx context.Context, owner, repo string, id int64) (io.ReadCloser, error) {
	target, resp, err := c.gh.Actions.DownloadArtifact(ctx, owner, repo, id, 2)
	return c.fetchRedirected(ctx, target, resp, err)
}
