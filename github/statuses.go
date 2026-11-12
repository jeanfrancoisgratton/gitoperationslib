package github

import (
	"context"
	"errors"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// checkState maps a check run's status and conclusion.
func checkState(status, conclusion string) gitoperationslib.StatusState {
	if status != "completed" {
		return gitoperationslib.StatusPending
	}
	switch conclusion {
	case "success", "neutral", "skipped":
		return gitoperationslib.StatusSuccess
	case "cancelled", "stale":
		return gitoperationslib.StatusError
	}
	return gitoperationslib.StatusFailure // failure, timed_out, action_required, startup_failure
}

// Status implements gitoperationslib.Commits. GitHub keeps two
// kinds: commit statuses, and check runs, which is how GitHub Actions reports;
// both are read (two requests, more for over 100 of either), and combined.
func (m commits) Status(ctx context.Context, owner, repo, ref string) (*gitoperationslib.CombinedStatus, error) {
	c := m.c
	out := &gitoperationslib.CombinedStatus{}
	var states []gitoperationslib.StatusState
	for opts := (&gh.ListOptions{PerPage: 100}); ; {
		cs, resp, err := c.gh.Repositories.GetCombinedStatus(ctx, owner, repo, ref, opts)
		if err != nil {
			return nil, convertError(err)
		}
		out.SHA = cs.GetSHA()
		for _, s := range cs.Statuses {
			st := gitoperationslib.StatusState(s.GetState())
			states = append(states, st)
			out.Statuses = append(out.Statuses, gitoperationslib.CommitStatus{
				Context: s.GetContext(), State: st, Description: s.GetDescription(), TargetURL: s.GetTargetURL(),
				CreatedAt: s.GetCreatedAt().Time, UpdatedAt: s.GetUpdatedAt().Time,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	for opts := (&gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: 100}}); ; {
		runs, resp, err := c.gh.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
		if err != nil {
			return nil, convertError(err)
		}
		for _, r := range runs.CheckRuns {
			st := checkState(r.GetStatus(), r.GetConclusion())
			states = append(states, st)
			if out.SHA == "" {
				out.SHA = r.GetHeadSHA()
			}
			desc := r.GetConclusion()
			if desc == "" {
				desc = r.GetStatus()
			}
			if t := r.GetOutput().GetTitle(); t != "" {
				desc = t
			}
			out.Statuses = append(out.Statuses, gitoperationslib.CommitStatus{
				Context: r.GetName(), State: st, Description: desc, TargetURL: r.GetHTMLURL(), Check: true,
				CreatedAt: r.GetStartedAt().Time, UpdatedAt: r.GetCompletedAt().Time,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	out.State = gitoperationslib.CombineStates(states...)
	return out, nil
}

// SetStatus implements gitoperationslib.Commits. It sets a commit
// status, not a check run: those belong to GitHub Apps.
func (m commits) SetStatus(ctx context.Context, owner, repo, sha string, s gitoperationslib.CommitStatus) error {
	c := m.c
	if s.Context == "" || s.State == "" {
		return errors.New("github: a commit status needs a context and a state")
	}
	if s.State == gitoperationslib.StatusWarning {
		return gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "statuses.set", "warning statuses")
	}
	st := gh.RepoStatus{State: gh.Ptr(string(s.State)), Context: gh.Ptr(s.Context)}
	if s.Description != "" {
		st.Description = gh.Ptr(s.Description)
	}
	if s.TargetURL != "" {
		st.TargetURL = gh.Ptr(s.TargetURL)
	}
	_, _, err := c.gh.Repositories.CreateStatus(ctx, owner, repo, sha, st)
	return convertError(err)
}
