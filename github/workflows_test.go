package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestWorkflowRunMapping(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":5,"run_number":2,"display_title":"t","path":".github/workflows/b.yml","event":"push","head_branch":"main","head_sha":"abc",
			"status":"completed","conclusion":"failure","actor":{"login":"bob"},"run_started_at":"2026-11-05T00:00:01Z"}`)
	})
	got, err := c.GetWorkflowRun(context.Background(), "a", "b", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 2 || got.Workflow != "b.yml" || got.Branch != "main" || got.Conclusion != "failure" || got.Actor != "bob" || got.StartedAt.IsZero() {
		t.Errorf("run = %+v", got)
	}
}

func TestListWorkflowRunsFilters(t *testing.T) {
	var paths []string
	status := http.StatusOK
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/v3/repos/a/b/actions/workflows/none.yml/runs" {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		fmt.Fprint(w, `{"total_count":0,"workflow_runs":[]}`)
	})
	ctx := context.Background()
	if _, err := c.ListWorkflowRuns(ctx, "a", "b", gitoperationslib.ListWorkflowRunsOptions{Number: 2}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Number: %v", err)
	}
	status = http.StatusNotFound
	page, err := c.ListWorkflowRuns(ctx, "a", "b", gitoperationslib.ListWorkflowRunsOptions{Workflow: "none.yml"})
	if err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Errorf("a workflow that does not exist: %+v, %v", page, err)
	}
}

func TestCancelFinishedRun(t *testing.T) {
	status := http.StatusConflict
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, `{"message":"Cannot cancel a workflow run that is completed."}`)
	})
	ctx := context.Background()
	if err := c.CancelWorkflowRun(ctx, "a", "b", 1); err != nil {
		t.Errorf("409: %v", err)
	}
	status = http.StatusAccepted
	if err := c.CancelWorkflowRun(ctx, "a", "b", 1); err != nil {
		t.Errorf("202: %v", err)
	}
	status = http.StatusNotFound
	if err := c.CancelWorkflowRun(ctx, "a", "b", 1); !gitoperationslib.IsNotFound(err) {
		t.Errorf("404: %v", err)
	}
}

func TestDeleteActiveWorkflowRun(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			t.Error("deleted a run still going")
		}
		fmt.Fprint(w, `{"id":1,"status":"in_progress"}`)
	})
	if err := c.DeleteWorkflowRun(context.Background(), "a", "b", 1); !errors.Is(err, gitoperationslib.ErrWorkflowRunActive) {
		t.Errorf("err = %v", err)
	}
}
