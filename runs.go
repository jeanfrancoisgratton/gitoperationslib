package gitoperationslib

import (
	"context"
	"io"
	"time"
)

// FollowLogOptions tunes FollowJobLog.
type FollowLogOptions struct {
	Interval time.Duration `json:"interval,omitzero"` // between polls; zero: two seconds
}

// WorkflowRuns reads and controls CI workflow runs and their jobs: Forgejo Actions
// and GitHub Actions (CapWorkflowRuns), through CI().Runs().
//
// On Forgejo, listing and reading runs need nothing more than the minimum version;
// jobs, logs, cancel and delete need 16; GetJob, both reruns and FollowJobLog
// (CapWorkflowLogFollow) need 17. Older servers return ErrNotSupported.
type WorkflowRuns interface {
	List(ctx context.Context, owner, repo string, opts ListWorkflowRunsOptions) (Page[WorkflowRun], error)
	Get(ctx context.Context, owner, repo string, id int64) (*WorkflowRun, error)
	// Cancel stops a run. A run already finished stays as it is, with
	// no error.
	Cancel(ctx context.Context, owner, repo string, id int64) error
	// Rerun starts a finished run's jobs again, under the same ID.
	Rerun(ctx context.Context, owner, repo string, id int64) error
	// Delete removes a finished run, and returns ErrWorkflowRunActive
	// for one still going, after one more request.
	Delete(ctx context.Context, owner, repo string, id int64) error

	// ListJobs lists a run's jobs. Forgejo returns them all at once, and
	// without their steps: use GetJob.
	ListJobs(ctx context.Context, owner, repo string, runID int64, opts ListOptions) (Page[WorkflowJob], error)
	GetJob(ctx context.Context, owner, repo string, jobID int64) (*WorkflowJob, error)
	// RerunJob starts a finished job again, with the jobs that depend on
	// it.
	RerunJob(ctx context.Context, owner, repo string, jobID int64) error
	// JobLogs streams a job's log, as text. GitHub can take a moment to
	// have it after the job ends. The caller must close it.
	JobLogs(ctx context.Context, owner, repo string, jobID int64) (io.ReadCloser, error)
	// Logs streams the logs of a run's jobs, as a ZIP archive. Forgejo
	// adds an empty ".MISSING" entry for a job with no log yet. The caller must
	// close it.
	Logs(ctx context.Context, owner, repo string, runID int64) (io.ReadCloser, error)

	// FollowJobLog writes the job's log to out as it grows, and returns once
	// the job ended and all of it is written, or when ctx is done. Forgejo needs
	// version 17, and sends the log as it comes. GitHub only has it once the job
	// ended: the call waits, then writes it all.
	FollowJobLog(ctx context.Context, owner, repo string, jobID int64, out io.Writer, opts FollowLogOptions) error
}

// ListWorkflowRunsOptions selects runs, newest first. Zero fields do not filter.
type ListWorkflowRunsOptions struct {
	ListOptions
	// Workflow is the workflow's file name, e.g. "build.yml". One that does not
	// exist selects nothing.
	Workflow string `json:"workflow,omitzero"`
	// Status is the forge's own word, as WorkflowRun.Status reports it. GitHub also
	// takes a conclusion, such as "failure". Forgejo refuses an unknown word (HTTP
	// 400).
	Status string `json:"status,omitzero"`
	Event  string `json:"event,omitzero"`  // what started it, e.g. "push"
	Branch string `json:"branch,omitzero"` // a branch name, not a ref
	SHA    string `json:"sha,omitzero"`    // the full commit
	// Number is WorkflowRun.Number. Forgejo only: GitHub returns ErrNotSupported.
	Number int64 `json:"number,omitzero"`
}

// WorkflowRun is a run of a CI workflow.
type WorkflowRun struct {
	ID       int64  `json:"id"`
	Number   int64  `json:"number"`
	Title    string `json:"title"`
	Workflow string `json:"workflow"` // the workflow's file, e.g. "build.yaml"
	Event    string `json:"event"`    // what started it, e.g. "push"
	Branch   string `json:"branch"`
	SHA      string `json:"sha"`
	// Status is the forge's own word: on Forgejo "waiting", "blocked", "running",
	// or the outcome; on GitHub "queued", "in_progress" or "completed", among others.
	Status string `json:"status"`
	// Conclusion is the outcome, such as "success", "failure", "cancelled" or
	// "skipped", and is empty until the run ends.
	Conclusion string    `json:"conclusion,omitzero"`
	Actor      string    `json:"actor"`
	HTMLURL    string    `json:"htmlUrl"`
	CreatedAt  time.Time `json:"createdAt,omitzero"`
	StartedAt  time.Time `json:"startedAt,omitzero"`
	StoppedAt  time.Time `json:"stoppedAt,omitzero"` // Forgejo only
	UpdatedAt  time.Time `json:"updatedAt,omitzero"`
}

// WorkflowJob is a job of a workflow run.
type WorkflowJob struct {
	ID      int64  `json:"id"`
	RunID   int64  `json:"runId"`
	Attempt int    `json:"attempt"`
	Name    string `json:"name"`
	// Status and Conclusion are as in WorkflowRun.
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion,omitzero"`
	Labels      []string  `json:"labels,omitzero"` // of the runners it asks for
	HTMLURL     string    `json:"htmlUrl"`
	StartedAt   time.Time `json:"startedAt,omitzero"`   // GitHub only
	CompletedAt time.Time `json:"completedAt,omitzero"` // GitHub only
	// Steps is set by GetJob, and by ListJobs on GitHub.
	Steps []WorkflowStep `json:"steps,omitzero"`
}

// WorkflowStep is a step of a workflow job.
type WorkflowStep struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	// Status and Conclusion are as in WorkflowRun.
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion,omitzero"`
	StartedAt   time.Time `json:"startedAt,omitzero"`
	CompletedAt time.Time `json:"completedAt,omitzero"`
}

// WorkflowArtifacts manages the files workflow runs store, through
// CI().Artifacts(): Forgejo 16 and later (ErrNotSupported on older servers) and
// GitHub (CapWorkflowArtifacts).
type WorkflowArtifacts interface {
	// List lists a repository's artifacts.
	List(ctx context.Context, owner, repo string, opts ListWorkflowArtifactsOptions) (Page[WorkflowArtifact], error)
	ListForRun(ctx context.Context, owner, repo string, runID int64, opts ListWorkflowArtifactsOptions) (Page[WorkflowArtifact], error)
	Get(ctx context.Context, owner, repo string, id int64) (*WorkflowArtifact, error)
	Delete(ctx context.Context, owner, repo string, id int64) error
	// Download streams an artifact as a ZIP archive. The caller
	// must close it.
	Download(ctx context.Context, owner, repo string, id int64) (io.ReadCloser, error)
}

// ListWorkflowArtifactsOptions selects artifacts.
type ListWorkflowArtifactsOptions struct {
	ListOptions
	Name string `json:"name,omitzero"` // the exact name; zero: all
}

// WorkflowArtifact is a file a workflow run stored.
type WorkflowArtifact struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"` // bytes
	RunID     int64     `json:"runId"`
	Expired   bool      `json:"expired,omitzero"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}
