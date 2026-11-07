package forgejo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestWorkflowVersions(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected %s %s", r.Method, r.URL) })
	ctx := context.Background()
	calls := map[string]func() error{
		"cancel":    func() error { return c.CancelWorkflowRun(ctx, "a", "b", 1) },
		"delete":    func() error { return c.DeleteWorkflowRun(ctx, "a", "b", 1) },
		"rerun":     func() error { return c.RerunWorkflowRun(ctx, "a", "b", 1) },
		"rerun job": func() error { return c.RerunWorkflowJob(ctx, "a", "b", 1) },
		"get job":   func() error { _, err := c.GetWorkflowJob(ctx, "a", "b", 1); return err },
		"jobs": func() error {
			_, err := c.ListWorkflowJobs(ctx, "a", "b", 1, gitoperationslib.ListOptions{})
			return err
		},
		"job logs": func() error { _, err := c.WorkflowJobLogs(ctx, "a", "b", 1); return err },
		"run logs": func() error { _, err := c.WorkflowRunLogs(ctx, "a", "b", 1); return err },
		"artifacts": func() error {
			_, err := c.ListWorkflowArtifacts(ctx, "a", "b", gitoperationslib.ListWorkflowArtifactsOptions{})
			return err
		},
		"by run": func() error {
			_, err := c.ListWorkflowRunArtifacts(ctx, "a", "b", 1, gitoperationslib.ListWorkflowArtifactsOptions{})
			return err
		},
		"artifact":  func() error { _, err := c.GetWorkflowArtifact(ctx, "a", "b", 1); return err },
		"del artif": func() error { return c.DeleteWorkflowArtifact(ctx, "a", "b", 1) },
		"download":  func() error { _, err := c.DownloadWorkflowArtifact(ctx, "a", "b", 1); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s on 15: %v", name, err)
		}
	}
	c.version = Version{16, 0, 7}
	for _, name := range []string{"rerun", "rerun job", "get job"} {
		if err := calls[name](); !errors.Is(err, gitoperationslib.ErrNotSupported) || !strings.Contains(err.Error(), "17.0.0") {
			t.Errorf("%s on 16: %v", name, err)
		}
	}
}

func TestListWorkflowRuns(t *testing.T) {
	var query string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		fmt.Fprint(w, `{"total_count":3,"workflow_runs":[{"id":4,"index_in_repo":2,"workflow_id":"b.yml","trigger_event":"push","event":"","prettyref":"main","commit_sha":"abc","status":"failure",
			"trigger_user":{"login":"bob"},"created":"2026-11-05T00:00:00Z","started":"2026-11-05T00:00:01Z","stopped":"1970-01-01T00:00:00Z"}]}`)
	})
	ctx := context.Background()
	page, err := c.ListWorkflowRuns(ctx, "acme", "widget", gitoperationslib.ListWorkflowRunsOptions{
		ListOptions: gitoperationslib.ListOptions{Page: 1, PerPage: 1}, Workflow: "b.yml", Status: "failure", Event: "push", Branch: "main", SHA: "abc", Number: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"workflow_id=b.yml", "status=failure", "event=push", "ref=refs%2Fheads%2Fmain", "head_sha=abc", "run_number=2", "limit=1", "page=1"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %s lacks %s", query, want)
		}
	}
	r := page.Items[0]
	if r.ID != 4 || r.Number != 2 || r.Workflow != "b.yml" || r.Event != "push" || r.Branch != "main" || r.Conclusion != "failure" || r.Actor != "bob" || r.StartedAt.IsZero() {
		t.Errorf("run = %+v", r)
	}
	if !r.StoppedAt.IsZero() {
		t.Errorf("1970 should read as never: %v", r.StoppedAt)
	}
	// The list only says its total.
	if page.NextPage != 2 {
		t.Errorf("NextPage = %d", page.NextPage)
	}
	last, err := c.ListWorkflowRuns(ctx, "acme", "widget", gitoperationslib.ListWorkflowRunsOptions{ListOptions: gitoperationslib.ListOptions{Page: 3, PerPage: 1}})
	if err != nil || last.NextPage != 0 {
		t.Errorf("last page = %+v, %v", last, err)
	}
	all, err := c.ListWorkflowRuns(ctx, "acme", "widget", gitoperationslib.ListWorkflowRunsOptions{})
	if err != nil || all.NextPage != 2 {
		t.Errorf("default page size, 1 of 3 = %+v, %v", all, err)
	}
}

func TestDeleteActiveWorkflowRun(t *testing.T) {
	status := "running"
	deleted := false
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fmt.Fprintf(w, `{"id":1,"status":%q}`, status)
	})
	c.version = Version{16, 0, 7}
	ctx := context.Background()
	if err := c.DeleteWorkflowRun(ctx, "a", "b", 1); !errors.Is(err, gitoperationslib.ErrWorkflowRunActive) || deleted {
		t.Errorf("running: %v, deleted %v", err, deleted)
	}
	status = "cancelled"
	if err := c.DeleteWorkflowRun(ctx, "a", "b", 1); err != nil || !deleted {
		t.Errorf("finished: %v, deleted %v", err, deleted)
	}
}

func TestWorkflowJobAndLogs(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/logs"):
			io.WriteString(w, "a log line")
		default:
			io.WriteString(w, `{"id":9,"run_id":4,"attempt":2,"name":"build","status":"running","runs_on":["self"],"html_url":"u",
				"steps":[{"number":1,"name":"s","status":"success","started":"2026-11-05T00:00:01Z","stopped":"2026-11-05T00:00:02Z"},{"number":2,"name":"t","status":"running","started":"2026-11-05T00:00:02Z"}]}`)
		}
	})
	c.version = Version{17, 0, 1}
	ctx := context.Background()
	job, err := c.GetWorkflowJob(ctx, "a", "b", 9)
	if err != nil {
		t.Fatal(err)
	}
	if job.RunID != 4 || job.Attempt != 2 || job.Conclusion != "" || len(job.Steps) != 2 || job.Steps[0].Conclusion != "success" || job.Steps[0].CompletedAt.IsZero() || job.Steps[1].Conclusion != "" {
		t.Errorf("job = %+v", job)
	}
	logs, err := c.WorkflowJobLogs(ctx, "a", "b", 9)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	if text, _ := io.ReadAll(logs); string(text) != "a log line" {
		t.Errorf("logs = %q", text)
	}
}
