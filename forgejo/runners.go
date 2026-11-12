package forgejo

import (
	"cmp"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

var (
	_ gitoperationslib.Runners = runners{}

	// workflowFollowVersion is the first release that reads a single job, which
	// following needs to know when it ends.
	workflowFollowVersion = Version{17, 0, 0}
)

type runners struct{ c *Client }

type runnerJSON struct {
	ID          int64    `json:"id"`
	UUID        string   `json:"uuid"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Labels      []string `json:"labels"`
	Version     string   `json:"version"`
	Ephemeral   bool     `json:"ephemeral"`
}

func (r *runnerJSON) toRunner() gitoperationslib.Runner {
	return gitoperationslib.Runner{
		ID: r.ID, UUID: r.UUID, Name: r.Name, Description: r.Description, Status: r.Status,
		Online: r.Status == "idle" || r.Status == "active", Busy: r.Status == "active",
		Labels: r.Labels, Version: r.Version, Ephemeral: r.Ephemeral,
	}
}

// runnersPath is where a scope's runners are: the API has the same calls below
// each of repos/{o}/{r}, orgs/{org}, user and admin.
func runnersPath(scope gitoperationslib.RunnerScope, rest ...string) (string, error) {
	var base []string
	switch {
	case scope.Instance && (scope.Owner != "" || scope.Repo != ""):
		return "", errors.New("forgejo: runners: an instance scope takes no owner or repository")
	case scope.Instance:
		base = []string{"admin"}
	case scope.Repo != "" && scope.Owner == "":
		return "", errors.New("forgejo: runners: a repository scope needs its owner")
	case scope.Repo != "":
		base = []string{"repos", scope.Owner, scope.Repo}
	case scope.Owner != "":
		base = []string{"orgs", scope.Owner}
	default:
		base = []string{"user"}
	}
	return forgejoapi.Path(append(append(base, "actions", "runners"), rest...)...), nil
}

func (svc runners) List(ctx context.Context, scope gitoperationslib.RunnerScope, opts gitoperationslib.ListRunnersOptions) (gitoperationslib.Page[gitoperationslib.Runner], error) {
	c := svc.c
	path, err := runnersPath(scope)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Runner]{}, err
	}
	q := forgejoapi.ListQuery(opts.ListOptions)
	if opts.Inherited {
		q.Set("visible", "true")
	}
	var out []runnerJSON
	resp, err := c.api.Do(ctx, http.MethodGet, path, q, nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Runner]{}, err
	}
	return toPage(resp, out, (*runnerJSON).toRunner), nil
}

func (svc runners) Get(ctx context.Context, scope gitoperationslib.RunnerScope, id int64) (*gitoperationslib.Runner, error) {
	c := svc.c
	path, err := runnersPath(scope, strconv.FormatInt(id, 10))
	if err != nil {
		return nil, err
	}
	var out runnerJSON
	if _, err := c.api.Do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	runner := out.toRunner()
	return &runner, nil
}

func (svc runners) Register(ctx context.Context, scope gitoperationslib.RunnerScope, opts gitoperationslib.RegisterRunnerOptions) (*gitoperationslib.RegisteredRunner, error) {
	c := svc.c
	const op = "runners.register"
	switch {
	case opts.Name == "":
		return nil, errors.New("forgejo: runners.register: name is required")
	case len(opts.Labels) > 0:
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "labels: a Forgejo runner declares its own")
	}
	path, err := runnersPath(scope)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": opts.Name, "description": opts.Description, "ephemeral": opts.Ephemeral}
	var out struct {
		ID    int64  `json:"id"`
		UUID  string `json:"uuid"`
		Token string `json:"token"`
	}
	if _, err := c.api.Do(ctx, http.MethodPost, path, nil, body, &out); err != nil {
		return nil, err
	}
	return &gitoperationslib.RegisteredRunner{ID: out.ID, UUID: out.UUID, Token: out.Token}, nil
}

func (svc runners) Delete(ctx context.Context, scope gitoperationslib.RunnerScope, id int64) error {
	c := svc.c
	path, err := runnersPath(scope, strconv.FormatInt(id, 10))
	if err != nil {
		return err
	}
	_, err = c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}

func (svc runners) RegistrationToken(ctx context.Context, scope gitoperationslib.RunnerScope) (*gitoperationslib.RunnerToken, error) {
	c := svc.c
	path, err := runnersPath(scope, "registration-token")
	if err != nil {
		return nil, err
	}
	var out struct {
		Token string `json:"token"`
	}
	if _, err := c.api.Do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &gitoperationslib.RunnerToken{Token: out.Token}, nil
}

// FollowJobLog asks for what follows what it already has (a Range
// request), checked on 17.0.1: the server answers 206 with the new part, 416 when
// there is nothing new, 404 "job has not been executed yet" before the job
// starts, and 500 once it started but has no log yet.
func (svc runs) FollowJobLog(ctx context.Context, owner, repo string, jobID int64, out io.Writer, opts gitoperationslib.FollowLogOptions) error {
	c := svc.c
	if err := c.needVersion(workflowFollowVersion, "workflows.follow_log", "following a job's log"); err != nil {
		return err
	}
	interval := cmp.Or(opts.Interval, 2*time.Second)
	path := jobPath(owner, repo, jobID, "logs")
	var offset int64
	fetch := func() error {
		body, status, err := c.api.StreamFrom(ctx, path, offset)
		switch code := gitoperationslib.StatusCode(err); {
		case code == http.StatusRequestedRangeNotSatisfiable,
			offset == 0 && (code == http.StatusNotFound || code == http.StatusInternalServerError):
			return nil
		case err != nil:
			return err
		}
		defer body.Close()
		if status == http.StatusOK && offset > 0 {
			// The whole log, despite the range: skip what was written already.
			if _, err := io.CopyN(io.Discard, body, offset); err != nil {
				return err
			}
		}
		n, err := io.Copy(out, body)
		offset += n
		return err
	}
	for {
		job, err := runs{c}.GetJob(ctx, owner, repo, jobID)
		if err != nil {
			return err
		}
		// Read after the status: once the job has ended, this gets the rest. A job
		// still waiting has nothing to read.
		if job.Status != "waiting" && job.Status != "blocked" {
			if err := fetch(); err != nil {
				return err
			}
		}
		if job.Conclusion != "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
