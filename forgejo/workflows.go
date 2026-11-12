package forgejo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

var (
	_ gitoperationslib.WorkflowRuns      = runs{}
	_ gitoperationslib.WorkflowArtifacts = artifacts{}

	// workflowJobsVersion has jobs, logs, cancel, delete and artifacts, and
	// workflowRerunVersion a single job and the reruns (measured on 15.0.11, 16.0.7
	// and 17.0.1: older servers answer 404).
	workflowJobsVersion  = Version{16, 0, 0}
	workflowRerunVersion = Version{17, 0, 0}
)

type runs struct{ c *Client }

type artifacts struct{ c *Client }

func (c *Client) needVersion(need Version, op, what string) error {
	if c.version.Compare(need) < 0 {
		return gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, what+" before Forgejo "+need.String())
	}
	return nil
}

// workflowTime drops the zero time, which versions before 17 send as 1970 for what
// has not happened yet.
func workflowTime(t *time.Time) time.Time {
	if t == nil || t.Unix() <= 0 {
		return time.Time{}
	}
	return *t
}

// workflowConclusion is the outcome in a Forgejo status, which has both in one word.
func workflowConclusion(status string) string {
	switch status {
	case "success", "failure", "cancelled", "skipped":
		return status
	}
	return ""
}

type workflowRunJSON struct {
	ID           int64      `json:"id"`
	IndexInRepo  int64      `json:"index_in_repo"`
	Title        string     `json:"title"`
	WorkflowID   string     `json:"workflow_id"`
	TriggerEvent string     `json:"trigger_event"`
	PrettyRef    string     `json:"prettyref"`
	CommitSHA    string     `json:"commit_sha"`
	Status       string     `json:"status"`
	TriggerUser  *loginJSON `json:"trigger_user"`
	HTMLURL      string     `json:"html_url"`
	Created      *time.Time `json:"created"`
	Started      *time.Time `json:"started"`
	Stopped      *time.Time `json:"stopped"`
	Updated      *time.Time `json:"updated"`
}

func (r *workflowRunJSON) toRun() gitoperationslib.WorkflowRun {
	return gitoperationslib.WorkflowRun{
		ID: r.ID, Number: r.IndexInRepo, Title: r.Title, Workflow: r.WorkflowID, Event: r.TriggerEvent, Branch: r.PrettyRef,
		SHA: r.CommitSHA, Status: r.Status, Conclusion: workflowConclusion(r.Status), Actor: r.TriggerUser.login(), HTMLURL: r.HTMLURL,
		CreatedAt: workflowTime(r.Created), StartedAt: workflowTime(r.Started), StoppedAt: workflowTime(r.Stopped), UpdatedAt: workflowTime(r.Updated),
	}
}

type workflowJobJSON struct {
	ID      int64    `json:"id"`
	RunID   int64    `json:"run_id"`
	Attempt int      `json:"attempt"`
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	RunsOn  []string `json:"runs_on"`
	HTMLURL string   `json:"html_url"`
	Steps   []struct {
		Number  int        `json:"number"`
		Name    string     `json:"name"`
		Status  string     `json:"status"`
		Started *time.Time `json:"started"`
		Stopped *time.Time `json:"stopped"`
	} `json:"steps"`
}

func (j *workflowJobJSON) toJob() gitoperationslib.WorkflowJob {
	job := gitoperationslib.WorkflowJob{
		ID: j.ID, RunID: j.RunID, Attempt: j.Attempt, Name: j.Name, Status: j.Status,
		Conclusion: workflowConclusion(j.Status), Labels: j.RunsOn, HTMLURL: j.HTMLURL,
	}
	for _, s := range j.Steps {
		job.Steps = append(job.Steps, gitoperationslib.WorkflowStep{
			Number: s.Number, Name: s.Name, Status: s.Status, Conclusion: workflowConclusion(s.Status),
			StartedAt: workflowTime(s.Started), CompletedAt: workflowTime(s.Stopped),
		})
	}
	return job
}

type workflowArtifactJSON struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Size      int64      `json:"size_in_bytes"`
	RunID     int64      `json:"run_id"`
	Expired   bool       `json:"expired"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (a *workflowArtifactJSON) toArtifact() gitoperationslib.WorkflowArtifact {
	return gitoperationslib.WorkflowArtifact{
		ID: a.ID, Name: a.Name, Size: a.Size, RunID: a.RunID, Expired: a.Expired,
		CreatedAt: workflowTime(a.CreatedAt), UpdatedAt: workflowTime(a.UpdatedAt), ExpiresAt: workflowTime(a.ExpiresAt),
	}
}

func runPath(owner, repo string, id int64, rest ...string) string {
	return repoPath(owner, repo, append([]string{"actions", "runs", strconv.FormatInt(id, 10)}, rest...)...)
}

func jobPath(owner, repo string, id int64, rest ...string) string {
	return repoPath(owner, repo, append([]string{"actions", "jobs", strconv.FormatInt(id, 10)}, rest...)...)
}

func artifactPath(owner, repo string, id int64, rest ...string) string {
	return repoPath(owner, repo, append([]string{"actions", "artifacts", strconv.FormatInt(id, 10)}, rest...)...)
}

func (svc runs) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListWorkflowRunsOptions) (gitoperationslib.Page[gitoperationslib.WorkflowRun], error) {
	c := svc.c
	q := forgejoapi.ListQuery(opts.ListOptions)
	for key, value := range map[string]string{"workflow_id": opts.Workflow, "status": opts.Status, "event": opts.Event, "head_sha": opts.SHA} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if opts.Branch != "" {
		// The server matches the whole ref: a bare name finds nothing.
		q.Set("ref", "refs/heads/"+opts.Branch)
	}
	if opts.Number != 0 {
		q.Set("run_number", strconv.FormatInt(opts.Number, 10))
	}
	var out struct {
		Runs  []workflowRunJSON `json:"workflow_runs"`
		Total int               `json:"total_count"`
	}
	if _, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "actions", "runs"), q, nil, &out); err != nil {
		return gitoperationslib.Page[gitoperationslib.WorkflowRun]{}, err
	}
	page := gitoperationslib.Page[gitoperationslib.WorkflowRun]{Items: make([]gitoperationslib.WorkflowRun, len(out.Runs))}
	for i := range out.Runs {
		page.Items[i] = out.Runs[i].toRun()
	}
	// This list sends no Link header, only its total.
	number, size := max(opts.Page, 1), opts.PerPage
	if size == 0 {
		size = len(out.Runs)
	}
	if number*size < out.Total {
		page.NextPage = number + 1
	}
	return page, nil
}

func (svc runs) Get(ctx context.Context, owner, repo string, id int64) (*gitoperationslib.WorkflowRun, error) {
	c := svc.c
	var out workflowRunJSON
	if _, err := c.api.Do(ctx, http.MethodGet, runPath(owner, repo, id), nil, nil, &out); err != nil {
		return nil, err
	}
	run := out.toRun()
	return &run, nil
}

func (svc runs) Cancel(ctx context.Context, owner, repo string, id int64) error {
	c := svc.c
	if err := c.needVersion(workflowJobsVersion, "workflows.cancel", "cancelling a run"); err != nil {
		return err
	}
	_, err := c.api.Do(ctx, http.MethodPost, runPath(owner, repo, id, "cancel"), nil, nil, nil)
	return err
}

func (svc runs) Rerun(ctx context.Context, owner, repo string, id int64) error {
	c := svc.c
	if err := c.needVersion(workflowRerunVersion, "workflows.rerun", "rerunning a run"); err != nil {
		return err
	}
	_, err := c.api.Do(ctx, http.MethodPost, runPath(owner, repo, id, "rerun"), nil, nil, nil)
	return err
}

// Delete reads the run first: the server answers HTTP 500 for one still
// going.
func (svc runs) Delete(ctx context.Context, owner, repo string, id int64) error {
	c := svc.c
	if err := c.needVersion(workflowJobsVersion, "workflows.delete", "deleting a run"); err != nil {
		return err
	}
	run, err := runs{c}.Get(ctx, owner, repo, id)
	if err != nil {
		return err
	}
	if run.Conclusion == "" {
		return fmt.Errorf("forgejo: workflows.delete: run %d is %s: %w", id, run.Status, gitoperationslib.ErrWorkflowRunActive)
	}
	_, err = c.api.Do(ctx, http.MethodDelete, runPath(owner, repo, id), nil, nil, nil)
	return err
}

func (svc runs) ListJobs(ctx context.Context, owner, repo string, runID int64, _ gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.WorkflowJob], error) {
	c := svc.c
	if err := c.needVersion(workflowJobsVersion, "workflows.jobs", "listing a run's jobs"); err != nil {
		return gitoperationslib.Page[gitoperationslib.WorkflowJob]{}, err
	}
	var out []workflowJobJSON
	if _, err := c.api.Do(ctx, http.MethodGet, runPath(owner, repo, runID, "jobs"), nil, nil, &out); err != nil {
		return gitoperationslib.Page[gitoperationslib.WorkflowJob]{}, err
	}
	page := gitoperationslib.Page[gitoperationslib.WorkflowJob]{Items: make([]gitoperationslib.WorkflowJob, len(out))}
	for i := range out {
		page.Items[i] = out[i].toJob()
	}
	return page, nil
}

func (svc runs) GetJob(ctx context.Context, owner, repo string, jobID int64) (*gitoperationslib.WorkflowJob, error) {
	c := svc.c
	if err := c.needVersion(workflowRerunVersion, "workflows.job", "reading a single job"); err != nil {
		return nil, err
	}
	var out workflowJobJSON
	if _, err := c.api.Do(ctx, http.MethodGet, jobPath(owner, repo, jobID), nil, nil, &out); err != nil {
		return nil, err
	}
	job := out.toJob()
	return &job, nil
}

func (svc runs) RerunJob(ctx context.Context, owner, repo string, jobID int64) error {
	c := svc.c
	if err := c.needVersion(workflowRerunVersion, "workflows.rerun_job", "rerunning a job"); err != nil {
		return err
	}
	_, err := c.api.Do(ctx, http.MethodPost, jobPath(owner, repo, jobID, "rerun"), nil, nil, nil)
	return err
}

func (svc runs) JobLogs(ctx context.Context, owner, repo string, jobID int64) (io.ReadCloser, error) {
	c := svc.c
	if err := c.needVersion(workflowJobsVersion, "workflows.job_logs", "reading a job's log"); err != nil {
		return nil, err
	}
	return c.api.Stream(ctx, jobPath(owner, repo, jobID, "logs"))
}

func (svc runs) Logs(ctx context.Context, owner, repo string, runID int64) (io.ReadCloser, error) {
	c := svc.c
	if err := c.needVersion(workflowJobsVersion, "workflows.run_logs", "reading a run's logs"); err != nil {
		return nil, err
	}
	return c.api.Stream(ctx, runPath(owner, repo, runID, "logs"))
}

func (c *Client) listArtifactsAt(ctx context.Context, path string, opts gitoperationslib.ListWorkflowArtifactsOptions) (gitoperationslib.Page[gitoperationslib.WorkflowArtifact], error) {
	if err := c.needVersion(workflowJobsVersion, "workflows.artifacts", "workflow artifacts"); err != nil {
		return gitoperationslib.Page[gitoperationslib.WorkflowArtifact]{}, err
	}
	q := forgejoapi.ListQuery(opts.ListOptions)
	if opts.Name != "" {
		q.Set("name", opts.Name)
	}
	var out []workflowArtifactJSON
	resp, err := c.api.Do(ctx, http.MethodGet, path, q, nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.WorkflowArtifact]{}, err
	}
	return toPage(resp, out, (*workflowArtifactJSON).toArtifact), nil
}

func (svc artifacts) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListWorkflowArtifactsOptions) (gitoperationslib.Page[gitoperationslib.WorkflowArtifact], error) {
	c := svc.c
	return c.listArtifactsAt(ctx, repoPath(owner, repo, "actions", "artifacts"), opts)
}

func (svc artifacts) ListForRun(ctx context.Context, owner, repo string, runID int64, opts gitoperationslib.ListWorkflowArtifactsOptions) (gitoperationslib.Page[gitoperationslib.WorkflowArtifact], error) {
	c := svc.c
	return c.listArtifactsAt(ctx, runPath(owner, repo, runID, "artifacts"), opts)
}

func (svc artifacts) Get(ctx context.Context, owner, repo string, id int64) (*gitoperationslib.WorkflowArtifact, error) {
	c := svc.c
	if err := c.needVersion(workflowJobsVersion, "workflows.artifact", "workflow artifacts"); err != nil {
		return nil, err
	}
	var out workflowArtifactJSON
	if _, err := c.api.Do(ctx, http.MethodGet, artifactPath(owner, repo, id), nil, nil, &out); err != nil {
		return nil, err
	}
	artifact := out.toArtifact()
	return &artifact, nil
}

func (svc artifacts) Delete(ctx context.Context, owner, repo string, id int64) error {
	c := svc.c
	if err := c.needVersion(workflowJobsVersion, "workflows.delete_artifact", "workflow artifacts"); err != nil {
		return err
	}
	_, err := c.api.Do(ctx, http.MethodDelete, artifactPath(owner, repo, id), nil, nil, nil)
	return err
}

func (svc artifacts) Download(ctx context.Context, owner, repo string, id int64) (io.ReadCloser, error) {
	c := svc.c
	if err := c.needVersion(workflowJobsVersion, "workflows.download_artifact", "workflow artifacts"); err != nil {
		return nil, err
	}
	return c.api.Stream(ctx, artifactPath(owner, repo, id, "zip"))
}
