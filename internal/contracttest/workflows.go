package contracttest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// WorkflowRuns drives real CI runs: env.Workflows must describe a runner. What a
// server's version lacks answers ErrNotSupported, and the rest of that part is
// skipped, so one test covers every version.
func WorkflowRuns(t *testing.T, env Env) {
	if env.Workflows == nil {
		t.Skip("no runner for this environment")
	}
	runs, ok := env.Client.(gitoperationslib.WorkflowRuns)
	if !ok {
		t.Skip("WorkflowRuns is not implemented")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	_, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	owner, w := env.Org, env.Workflows
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if w.Prepare != nil {
		check(w.Prepare(ctx, owner, *repo))
	}
	info, err := env.Client.Repos().Get(ctx, owner, *repo)
	check(err)
	base := info.DefaultBranch

	const marker = "gitopslib-marker-7f3a"
	workflow := func(file, branch, steps string) {
		t.Helper()
		content := fmt.Sprintf("on:\n  push:\n    branches: [%s]\njobs:\n  work:\n    runs-on: %s\n    steps:\n%s", branch, w.RunsOn, steps)
		check(retryConflict(ctx, t, func() error { return env.WriteFile(ctx, owner, *repo, base, w.Dir+"/"+file, content) }))
	}
	workflow("ok.yml", "run-ok", fmt.Sprintf("      - run: echo %s > out.txt && cat out.txt\n      - uses: %s\n        with:\n          name: outfile\n          path: out.txt\n", marker, w.UploadArtifact))
	workflow("slow.yml", "run-slow", "      - run: sleep 25\n")

	// Waits for the only run of a branch to satisfy done.
	await := func(branch string, done func(gitoperationslib.WorkflowRun) bool) gitoperationslib.WorkflowRun {
		t.Helper()
		for {
			page, err := runs.ListWorkflowRuns(ctx, owner, *repo, gitoperationslib.ListWorkflowRunsOptions{Branch: branch})
			check(err)
			if len(page.Items) > 0 && done(page.Items[0]) {
				return page.Items[0]
			}
			select {
			case <-ctx.Done():
				t.Fatalf("waiting for a run on %s: %v, last %+v", branch, ctx.Err(), page.Items)
			case <-time.After(3 * time.Second):
			}
		}
	}
	finished := func(r gitoperationslib.WorkflowRun) bool { return r.Conclusion != "" }
	// Follows one run: a new branch and its commit can be two pushes, so two runs.
	awaitRun := func(id int64, done func(*gitoperationslib.WorkflowRun) bool) *gitoperationslib.WorkflowRun {
		t.Helper()
		for {
			r, err := runs.GetWorkflowRun(ctx, owner, *repo, id)
			check(err)
			if done(r) {
				return r
			}
			select {
			case <-ctx.Done():
				t.Fatalf("waiting for run %d: %v, last %+v", id, ctx.Err(), r)
			case <-time.After(3 * time.Second):
			}
		}
	}
	started := func(gitoperationslib.WorkflowRun) bool { return true }

	check(retryConflict(ctx, t, func() error { return env.CreateBranch(ctx, owner, *repo, "run-ok", "ok.txt", "x\n") }))
	run := await("run-ok", finished)
	if run.ID == 0 || run.Number == 0 || run.Title == "" || run.Workflow != "ok.yml" || run.Event != "push" || run.Branch != "run-ok" || run.SHA == "" ||
		run.Conclusion != "success" || run.Actor == "" || run.HTMLURL == "" || run.CreatedAt.IsZero() || run.StartedAt.IsZero() {
		t.Errorf("run = %+v", run)
	}
	got, err := runs.GetWorkflowRun(ctx, owner, *repo, run.ID)
	check(err)
	if got.ID != run.ID || got.Conclusion != run.Conclusion {
		t.Errorf("Get = %+v", got)
	}
	if _, err := runs.GetWorkflowRun(ctx, owner, *repo, 99999999); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(missing): err = %v, want 404", err)
	}

	t.Run("filters", func(t *testing.T) {
		ids := func(opts gitoperationslib.ListWorkflowRunsOptions) []int64 {
			t.Helper()
			page, err := runs.ListWorkflowRuns(ctx, owner, *repo, opts)
			check(err)
			out := []int64{}
			for _, r := range page.Items {
				out = append(out, r.ID)
			}
			return out
		}
		for name, opts := range map[string]gitoperationslib.ListWorkflowRunsOptions{
			"workflow": {Workflow: "ok.yml"}, "status": {Status: run.Status}, "event": {Event: "push"}, "branch": {Branch: "run-ok"}, "sha": {SHA: run.SHA},
		} {
			if got := ids(opts); !slices.Contains(got, run.ID) {
				t.Errorf("%s: %v lacks run %d", name, got, run.ID)
			}
		}
		for name, opts := range map[string]gitoperationslib.ListWorkflowRunsOptions{
			"workflow": {Workflow: "no-such.yml"}, "event": {Event: "schedule"}, "branch": {Branch: "no-such"}, "sha": {SHA: strings.Repeat("0", 40)},
		} {
			if got := ids(opts); len(got) != 0 {
				t.Errorf("%s: %v, want nothing", name, got)
			}
		}
		_, err := runs.ListWorkflowRuns(ctx, owner, *repo, gitoperationslib.ListWorkflowRunsOptions{Number: run.Number})
		if err != nil && !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("Number: %v", err)
		}
		// Every page of a list that only says its total.
		seen := 0
		for _, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.WorkflowRun], error) {
			return runs.ListWorkflowRuns(ctx, owner, *repo, gitoperationslib.ListWorkflowRunsOptions{ListOptions: o})
		}) {
			check(err)
			seen++
		}
		if seen == 0 {
			t.Error("All saw no runs")
		}
	})

	read := func(open func() (io.ReadCloser, error)) ([]byte, error) {
		t.Helper()
		// GitHub can take a while to have a finished job's log.
		deadline := time.Now().Add(time.Minute)
		for {
			r, err := open()
			if err == nil {
				defer r.Close()
				return io.ReadAll(r)
			}
			if !gitoperationslib.IsNotFound(err) || time.Now().After(deadline) {
				return nil, err
			}
			time.Sleep(5 * time.Second)
		}
	}

	var job gitoperationslib.WorkflowJob
	t.Run("jobs and logs", func(t *testing.T) {
		page, err := runs.ListWorkflowJobs(ctx, owner, *repo, run.ID, gitoperationslib.ListOptions{})
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Skip(err)
		}
		check(err)
		if len(page.Items) != 1 || page.Items[0].Name != "work" || page.Items[0].RunID != run.ID || page.Items[0].Conclusion != "success" || page.Items[0].ID == 0 {
			t.Fatalf("jobs = %+v", page.Items)
		}
		job = page.Items[0]
		logs, err := read(func() (io.ReadCloser, error) { return runs.WorkflowJobLogs(ctx, owner, *repo, job.ID) })
		check(err)
		if !strings.Contains(string(logs), marker) {
			t.Errorf("job log lacks the marker: %.300s", logs)
		}
		archive, err := read(func() (io.ReadCloser, error) { return runs.WorkflowRunLogs(ctx, owner, *repo, run.ID) })
		check(err)
		if !strings.HasPrefix(string(archive), "PK") {
			t.Errorf("run logs are not a ZIP: %.40q", archive)
		}
		if _, err := runs.ListWorkflowJobs(ctx, owner, *repo, 99999999, gitoperationslib.ListOptions{}); !gitoperationslib.IsNotFound(err) {
			t.Errorf("jobs of a missing run: err = %v, want 404", err)
		}
		single, err := runs.GetWorkflowJob(ctx, owner, *repo, job.ID)
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			return
		}
		check(err)
		if single.ID != job.ID || len(single.Steps) == 0 || single.Steps[0].Name == "" {
			t.Errorf("job = %+v", single)
		}
	})

	t.Run("artifacts", func(t *testing.T) {
		a, ok := env.Client.(gitoperationslib.WorkflowArtifacts)
		if !ok {
			t.Skip("WorkflowArtifacts is not implemented")
		}
		list, err := a.ListWorkflowRunArtifacts(ctx, owner, *repo, run.ID, gitoperationslib.ListWorkflowArtifactsOptions{})
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Skip(err)
		}
		check(err)
		if len(list.Items) != 1 || list.Items[0].Name != "outfile" || list.Items[0].RunID != run.ID || list.Items[0].Size == 0 || list.Items[0].ExpiresAt.IsZero() {
			t.Fatalf("artifacts = %+v", list.Items)
		}
		artifact := list.Items[0]
		for name, opts := range map[string]gitoperationslib.ListWorkflowArtifactsOptions{"outfile": {Name: "outfile"}, "no-such": {Name: "no-such"}} {
			page, err := a.ListWorkflowArtifacts(ctx, owner, *repo, opts)
			check(err)
			byRun, err := a.ListWorkflowRunArtifacts(ctx, owner, *repo, run.ID, opts)
			check(err)
			has := slices.ContainsFunc(page.Items, func(x gitoperationslib.WorkflowArtifact) bool { return x.ID == artifact.ID })
			if want := name == "outfile"; has != want || (len(byRun.Items) == 1) != want {
				t.Errorf("name %s: %+v, by run %+v", name, page.Items, byRun.Items)
			}
		}
		got, err := a.GetWorkflowArtifact(ctx, owner, *repo, artifact.ID)
		check(err)
		if got.Name != "outfile" {
			t.Errorf("Get = %+v", got)
		}
		zip, err := read(func() (io.ReadCloser, error) { return a.DownloadWorkflowArtifact(ctx, owner, *repo, artifact.ID) })
		check(err)
		if !strings.HasPrefix(string(zip), "PK") {
			t.Errorf("artifact is not a ZIP: %.40q", zip)
		}
		check(a.DeleteWorkflowArtifact(ctx, owner, *repo, artifact.ID))
		if _, err := a.GetWorkflowArtifact(ctx, owner, *repo, artifact.ID); !gitoperationslib.IsNotFound(err) {
			t.Errorf("Get after delete: err = %v, want 404", err)
		}
	})

	t.Run("cancel and delete", func(t *testing.T) {
		check(retryConflict(ctx, t, func() error { return env.CreateBranch(ctx, owner, *repo, "run-slow", "slow.txt", "x\n") }))
		slow := await("run-slow", started)
		err := runs.DeleteWorkflowRun(ctx, owner, *repo, slow.ID)
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Skip(err)
		}
		if !errors.Is(err, gitoperationslib.ErrWorkflowRunActive) {
			t.Errorf("Delete(active): err = %v, want ErrWorkflowRunActive", err)
		}
		check(runs.CancelWorkflowRun(ctx, owner, *repo, slow.ID))
		stopped := awaitRun(slow.ID, func(r *gitoperationslib.WorkflowRun) bool { return r.Conclusion != "" })
		if stopped.Conclusion != "cancelled" {
			t.Errorf("cancelled run = %+v", stopped)
		}
		check(runs.CancelWorkflowRun(ctx, owner, *repo, slow.ID)) // already finished: no error
		check(runs.DeleteWorkflowRun(ctx, owner, *repo, slow.ID))
		if _, err := runs.GetWorkflowRun(ctx, owner, *repo, slow.ID); !gitoperationslib.IsNotFound(err) {
			t.Errorf("Get after delete: err = %v, want 404", err)
		}
	})

	t.Run("follow a log", func(t *testing.T) {
		f, ok := env.Client.(gitoperationslib.WorkflowLogFollower)
		if !ok {
			t.Skip("WorkflowLogFollower is not implemented")
		}
		workflow("follow.yml", "run-follow", "      - run: for i in 1 2 3 4 5 6; do echo follow-line-$i; sleep 3; done\n")
		check(retryConflict(ctx, t, func() error { return env.CreateBranch(ctx, owner, *repo, "run-follow", "follow.txt", "x\n") }))
		r := await("run-follow", started)
		var jobs gitoperationslib.Page[gitoperationslib.WorkflowJob]
		for len(jobs.Items) == 0 {
			var err error
			jobs, err = runs.ListWorkflowJobs(ctx, owner, *repo, r.ID, gitoperationslib.ListOptions{})
			if errors.Is(err, gitoperationslib.ErrNotSupported) {
				t.Skip(err)
			}
			check(err)
			time.Sleep(time.Second)
		}
		var out strings.Builder
		err := f.FollowWorkflowJobLog(ctx, owner, *repo, jobs.Items[0].ID, &out, gitoperationslib.FollowLogOptions{Interval: time.Second})
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Skip(err)
		}
		check(err)
		job, err := runs.GetWorkflowJob(ctx, owner, *repo, jobs.Items[0].ID)
		check(err)
		if job.Conclusion == "" {
			t.Errorf("returned before the job ended: %+v", job)
		}
		text, last := out.String(), -1
		for i := 1; i <= 6; i++ {
			at := strings.Index(text, fmt.Sprintf("follow-line-%d", i))
			if at <= last {
				t.Fatalf("line %d missing or out of order in %q", i, text)
			}
			last = at
		}
	})

	t.Run("rerun", func(t *testing.T) {
		err := runs.RerunWorkflowRun(ctx, owner, *repo, run.ID)
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Skip(err)
		}
		check(err)
		again := awaitRun(run.ID, func(r *gitoperationslib.WorkflowRun) bool {
			return r.Conclusion != "" && r.UpdatedAt.After(run.UpdatedAt)
		})
		if again.Conclusion != "success" {
			t.Errorf("rerun = %+v", again)
		}
		if job.ID == 0 {
			return
		}
		page, err := runs.ListWorkflowJobs(ctx, owner, *repo, run.ID, gitoperationslib.ListOptions{})
		check(err)
		before := again.UpdatedAt
		check(runs.RerunWorkflowJob(ctx, owner, *repo, page.Items[0].ID))
		awaitRun(run.ID, func(r *gitoperationslib.WorkflowRun) bool {
			return r.Conclusion != "" && r.UpdatedAt.After(before)
		})
	})
}
