package github

import (
	"cmp"
	"context"
	"errors"
	"io"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var (
	_ gitoperationslib.Runners             = (*Client)(nil)
	_ gitoperationslib.WorkflowLogFollower = (*Client)(nil)
)

func toRunner(r *gh.Runner) gitoperationslib.Runner {
	runner := gitoperationslib.Runner{
		ID: r.GetID(), Name: r.GetName(), Status: r.GetStatus(), Online: r.GetStatus() == "online", Busy: r.GetBusy(),
		Version: r.GetVersion(), Ephemeral: r.GetEphemeral(), OS: r.GetOS(),
	}
	for _, l := range r.Labels {
		runner.Labels = append(runner.Labels, l.GetName())
	}
	return runner
}

// runnerScope checks the scope, and says whether it is an organisation's. GitHub
// has no user runners, and an enterprise's are out of scope.
func runnerScope(scope gitoperationslib.RunnerScope, op string) (org bool, err error) {
	switch {
	case scope.Instance:
		return false, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "instance runners")
	case scope.Owner == "":
		return false, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "a user's runners")
	}
	return scope.Repo == "", nil
}

func (c *Client) ListRunners(ctx context.Context, scope gitoperationslib.RunnerScope, opts gitoperationslib.ListRunnersOptions) (gitoperationslib.Page[gitoperationslib.Runner], error) {
	const op = "runners.list"
	org, err := runnerScope(scope, op)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Runner]{}, err
	}
	if opts.Inherited {
		return gitoperationslib.Page[gitoperationslib.Runner]{}, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "inherited runners")
	}
	query := &gh.ListRunnersOptions{ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage}}
	var list *gh.Runners
	var resp *gh.Response
	if org {
		list, resp, err = c.gh.Actions.ListOrganizationRunners(ctx, scope.Owner, query)
	} else {
		list, resp, err = c.gh.Actions.ListRunners(ctx, scope.Owner, scope.Repo, query)
	}
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Runner]{}, convertError(err)
	}
	page := gitoperationslib.Page[gitoperationslib.Runner]{Items: make([]gitoperationslib.Runner, len(list.Runners)), NextPage: resp.NextPage}
	for i, r := range list.Runners {
		page.Items[i] = toRunner(r)
	}
	return page, nil
}

func (c *Client) GetRunner(ctx context.Context, scope gitoperationslib.RunnerScope, id int64) (*gitoperationslib.Runner, error) {
	org, err := runnerScope(scope, "runners.get")
	if err != nil {
		return nil, err
	}
	var r *gh.Runner
	if org {
		r, _, err = c.gh.Actions.GetOrganizationRunner(ctx, scope.Owner, id)
	} else {
		r, _, err = c.gh.Actions.GetRunner(ctx, scope.Owner, scope.Repo, id)
	}
	if err != nil {
		return nil, convertError(err)
	}
	runner := toRunner(r)
	return &runner, nil
}

// RegisterRunner makes a just-in-time configuration, which is single-use: the only
// registration GitHub's API offers.
func (c *Client) RegisterRunner(ctx context.Context, scope gitoperationslib.RunnerScope, opts gitoperationslib.RegisterRunnerOptions) (*gitoperationslib.RegisteredRunner, error) {
	const op = "runners.register"
	org, err := runnerScope(scope, op)
	if err != nil {
		return nil, err
	}
	switch {
	case opts.Name == "":
		return nil, errors.New("github: runners.register: name is required")
	case !opts.Ephemeral:
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "lasting runners: use RunnerRegistrationToken with the runner's configuration script")
	case opts.Description != "":
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "runner descriptions")
	}
	labels := opts.Labels
	if len(labels) == 0 {
		labels = []string{"self-hosted"}
	}
	// Runner group 1 is the default one, the only one below an enterprise.
	body := gh.CreateJITConfigRequest{Name: opts.Name, RunnerGroupID: 1, Labels: labels}
	var jit *gh.JITRunnerConfig
	if org {
		jit, _, err = c.gh.Actions.CreateOrgJITConfig(ctx, scope.Owner, body)
	} else {
		jit, _, err = c.gh.Actions.CreateRepoJITConfig(ctx, scope.Owner, scope.Repo, body)
	}
	if err != nil {
		return nil, convertError(err)
	}
	return &gitoperationslib.RegisteredRunner{ID: jit.GetRunner().GetID(), JITConfig: jit.GetEncodedJITConfig()}, nil
}

// DeleteRunner reads the runner first: GitHub answers 204 for one that does not
// exist.
func (c *Client) DeleteRunner(ctx context.Context, scope gitoperationslib.RunnerScope, id int64) error {
	if _, err := c.GetRunner(ctx, scope, id); err != nil {
		return err
	}
	var err error
	if scope.Repo == "" {
		_, err = c.gh.Actions.RemoveOrganizationRunner(ctx, scope.Owner, id)
	} else {
		_, err = c.gh.Actions.RemoveRunner(ctx, scope.Owner, scope.Repo, id)
	}
	return convertError(err)
}

func (c *Client) RunnerRegistrationToken(ctx context.Context, scope gitoperationslib.RunnerScope) (*gitoperationslib.RunnerToken, error) {
	org, err := runnerScope(scope, "runners.registration_token")
	if err != nil {
		return nil, err
	}
	var t *gh.RegistrationToken
	if org {
		t, _, err = c.gh.Actions.CreateOrganizationRegistrationToken(ctx, scope.Owner)
	} else {
		t, _, err = c.gh.Actions.CreateRegistrationToken(ctx, scope.Owner, scope.Repo)
	}
	if err != nil {
		return nil, convertError(err)
	}
	return &gitoperationslib.RunnerToken{Token: t.GetToken(), ExpiresAt: t.GetExpiresAt().Time}, nil
}

// FollowWorkflowJobLog waits for the job to end: GitHub stores a job's log only
// then (checked on 2026-11-11: while it runs, the log's address answers 404). It
// then writes it all, waiting a little more if it is not stored yet.
func (c *Client) FollowWorkflowJobLog(ctx context.Context, owner, repo string, jobID int64, out io.Writer, opts gitoperationslib.FollowLogOptions) error {
	interval := cmp.Or(opts.Interval, 2*time.Second)
	wait := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			return nil
		}
	}
	for {
		job, err := c.GetWorkflowJob(ctx, owner, repo, jobID)
		if err != nil {
			return err
		}
		if job.Conclusion != "" {
			break
		}
		if err := wait(); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(time.Minute)
	for {
		log, err := c.WorkflowJobLogs(ctx, owner, repo, jobID)
		if err == nil {
			defer log.Close()
			_, err = io.Copy(out, log)
			return err
		}
		if !gitoperationslib.IsNotFound(err) || time.Now().After(deadline) {
			return err
		}
		if err := wait(); err != nil {
			return err
		}
	}
}
